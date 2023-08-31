package drr

import (
	"math"
	"time"

	"github.com/iotaledger/hive.go/core/safemath"
	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/congestioncontrol/scheduler"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
)

type Deficit int64

type SubSlotIndex int

type Scheduler struct {
	events *scheduler.Events

	quantumFunc func(iotago.AccountID, iotago.SlotIndex) (Deficit, error)

	latestCommittedSlot func() iotago.SlotIndex

	apiProvider api.Provider

	buffer          *BufferQueue
	validatorBuffer map[iotago.AccountID]*ValidatorQueue
	bufferMutex     syncutils.RWMutex

	deficits *shrinkingmap.ShrinkingMap[iotago.AccountID, Deficit]

	tokenBucket      float64
	lastScheduleTime time.Time

	shutdownSignal chan struct{}

	blockChan chan *blocks.Block

	blockCache *blocks.Blocks

	validationBlocksPerSlot SubSlotIndex // TODO: make this a protocol parameter

	blacklist map[iotago.AccountID]struct{}

	validationBlocks map[iotago.SlotIndex]map[SubSlotIndex]*blocks.Block

	errorHandler func(error)

	module.Module
}

func NewProvider(opts ...options.Option[Scheduler]) module.Provider[*engine.Engine, scheduler.Scheduler] {
	return module.Provide(func(e *engine.Engine) scheduler.Scheduler {
		s := New(e, opts...)
		s.errorHandler = e.ErrorHandler("scheduler")
		s.buffer = NewBufferQueue(int(s.apiProvider.CurrentAPI().ProtocolParameters().CongestionControlParameters().MaxBufferSize))

		e.HookConstructed(func() {
			s.blockCache = e.BlockCache
			e.Events.Scheduler.LinkTo(s.events)
			e.Ledger.HookInitialized(func() {
				// quantum retrieve function gets the account's Mana and returns the quantum for that account
				s.quantumFunc = func(accountID iotago.AccountID, manaSlot iotago.SlotIndex) (Deficit, error) {
					mana, err := e.Ledger.ManaManager().GetManaOnAccount(accountID, manaSlot)
					if err != nil {
						return 0, err
					}

					minMana := s.apiProvider.CurrentAPI().ProtocolParameters().CongestionControlParameters().MinMana
					if mana < minMana {
						return 0, ierrors.Errorf("account %s has insufficient Mana for block to be scheduled: account Mana %d, min Mana %d", accountID, mana, minMana)
					}

					return Deficit(mana / minMana), nil
				}
				s.latestCommittedSlot = func() iotago.SlotIndex {
					return e.Storage.Settings().LatestCommitment().Index()
				}
			})
			s.TriggerConstructed()
			e.Events.Booker.BlockBooked.Hook(func(block *blocks.Block) {
				s.AddBlock(block)
				s.selectBlockToScheduleWithLocking()

			})
			e.Events.Ledger.AccountCreated.Hook(func(accountID iotago.AccountID) {
				s.bufferMutex.Lock()
				defer s.bufferMutex.Unlock()

				s.createIssuer(accountID)
			})
			e.Events.Ledger.AccountDestroyed.Hook(func(accountID iotago.AccountID) {
				s.bufferMutex.Lock()
				defer s.bufferMutex.Unlock()

				s.removeIssuer(accountID, ierrors.New("account destroyed"))
			})

			e.HookInitialized(s.Start)
		})

		return s
	})
}

func New(apiProvider api.Provider, opts ...options.Option[Scheduler]) *Scheduler {
	return options.Apply(
		&Scheduler{
			events:           scheduler.NewEvents(),
			lastScheduleTime: time.Now(),
			deficits:         shrinkingmap.New[iotago.AccountID, Deficit](),
			apiProvider:      apiProvider,
		}, opts,
	)
}

func (s *Scheduler) subSlotIndex(block *blocks.Block) SubSlotIndex {
	apiForBlock, err := s.apiProvider.APIForVersion(block.ProtocolBlock().ProtocolVersion)
	if err != nil {
		s.errorHandler(ierrors.Wrapf(err, "failed to get API for version %d", block.ProtocolBlock().ProtocolVersion))
	}
	blockSlot := apiForBlock.TimeProvider().SlotFromTime(block.IssuingTime())
	apiForBlock.TimeProvider().SlotStartTime(blockSlot)
	timeSinceSlotStart := block.IssuingTime().Sub(apiForBlock.TimeProvider().SlotStartTime(blockSlot))

	return SubSlotIndex(timeSinceSlotStart.Seconds() * float64(s.validationBlocksPerSlot) / float64(apiForBlock.TimeProvider().SlotDurationSeconds()))
}

func (s *Scheduler) Shutdown() {
	close(s.shutdownSignal)
	s.TriggerStopped()
}

// Start starts the scheduler.
func (s *Scheduler) Start() {
	s.shutdownSignal = make(chan struct{}, 1)
	s.blockChan = make(chan *blocks.Block, 1)
	go s.mainLoop()

	s.TriggerInitialized()
}

// Rate gets the rate of the scheduler in units of work per second.
func (s *Scheduler) Rate() iotago.WorkScore {
	return s.apiProvider.CurrentAPI().ProtocolParameters().CongestionControlParameters().SchedulerRate
}

// IssuerQueueSizeCount returns the queue size of the given issuer as block count.
func (s *Scheduler) IssuerQueueBlockCount(issuerID iotago.AccountID) int {
	s.bufferMutex.RLock()
	defer s.bufferMutex.RUnlock()

	return s.buffer.IssuerQueue(issuerID).Size()
}

// IssuerQueueWork returns the queue size of the given issuer in work units.
func (s *Scheduler) IssuerQueueWork(issuerID iotago.AccountID) iotago.WorkScore {
	s.bufferMutex.RLock()
	defer s.bufferMutex.RUnlock()

	return s.buffer.IssuerQueue(issuerID).Work()
}

// BufferSize returns the current buffer size of the Scheduler as block count.
func (s *Scheduler) BufferSize() int {
	s.bufferMutex.RLock()
	defer s.bufferMutex.RUnlock()

	return s.buffer.Size()
}

// MaxBufferSize returns the max buffer size of the Scheduler as block count.
func (s *Scheduler) MaxBufferSize() int {
	return int(s.apiProvider.CurrentAPI().ProtocolParameters().CongestionControlParameters().MaxBufferSize)
}

// ReadyBlocksCount returns the number of ready blocks.
func (s *Scheduler) ReadyBlocksCount() int {
	s.bufferMutex.RLock()
	defer s.bufferMutex.RUnlock()

	return s.buffer.ReadyBlocksCount()
}

func (s *Scheduler) IsBlockIssuerReady(accountID iotago.AccountID, blocks ...*blocks.Block) bool {
	s.bufferMutex.RLock()
	defer s.bufferMutex.RUnlock()

	// if the buffer is completely empty, any issuer can issue a block.
	if s.buffer.Size() == 0 {
		return true
	}
	work := iotago.WorkScore(0)
	// if no specific block(s) is provided, assume max block size
	currentAPI := s.apiProvider.CurrentAPI()
	if len(blocks) == 0 {
		work = currentAPI.MaxBlockWork()
	}
	for _, block := range blocks {
		work += block.WorkScore()
	}
	deficit, exists := s.deficits.Get(accountID)
	if !exists {
		return false
	}

	return deficit >= s.deficitFromWork(work+s.buffer.IssuerQueue(accountID).Work())
}

func (s *Scheduler) AddBlock(block *blocks.Block) {
	if _, isBasic := block.BasicBlock(); isBasic {
		s.enqueueBasicBlock(block)
	} else if _, isValidation := block.ValidationBlock(); isValidation {
		s.enqueueValidationBlock(block)
	} else {
		panic("invalid block type")
	}
}

func (s *Scheduler) enqueueBasicBlock(block *blocks.Block) {
	s.bufferMutex.Lock()
	defer s.bufferMutex.Unlock()

	slotIndex := s.latestCommittedSlot()

	issuerID := block.ProtocolBlock().IssuerID
	issuerQueue, err := s.buffer.GetIssuerQueue(issuerID)
	if err != nil {
		// this should only ever happen if the issuer has been removed due to insufficient Mana.
		// if Mana is now sufficient again, we can add the issuer again.
		_, quantumErr := s.quantumFunc(issuerID, slotIndex)
		if quantumErr != nil {
			s.errorHandler(ierrors.Wrapf(quantumErr, "failed to retrieve quantum for issuerID %s in slot %d when adding a block", issuerID, slotIndex))
		}

		issuerQueue = s.createIssuer(issuerID)
	}

	droppedBlocks, err := s.buffer.Submit(block, issuerQueue, func(issuerID iotago.AccountID) Deficit {
		quantum, quantumErr := s.quantumFunc(issuerID, slotIndex)
		if quantumErr != nil {
			s.errorHandler(ierrors.Wrapf(quantumErr, "failed to retrieve deficit for issuerID %d in slot %d when submitting a block", issuerID, slotIndex))

			return 0
		}

		return quantum
	})
	// error submitting indicates that the block was already submitted so we do nothing else.
	if err != nil {
		return
	}
	for _, b := range droppedBlocks {
		b.SetDropped()
		s.events.BlockDropped.Trigger(b, ierrors.New("block dropped from buffer"))
	}
	if block.SetEnqueued() {
		s.events.BlockEnqueued.Trigger(block)
		s.tryReady(block)
	}
}

func (s *Scheduler) enqueueValidationBlock(block *blocks.Block) {
	s.bufferMutex.Lock()
	defer s.bufferMutex.Unlock()

	if _, exists := s.blacklist[block.ProtocolBlock().IssuerID]; exists {
		// this validator is blacklisted, so skip this block
		return
	}

	// TODO: double check that the validator is in the committee for this epoch?
	validatorQueue, exists := s.validatorBuffer[block.ProtocolBlock().IssuerID]
	if !exists {
		validatorQueue = NewValidatorQueue(block.ProtocolBlock().IssuerID)
		s.validatorBuffer[block.ProtocolBlock().IssuerID] = validatorQueue
	}
	validatorQueue.Submit(block)

	if slotValidationBlocks := s.validationBlocks[block.ID().Index()]; slotValidationBlocks != nil {
		subSlot := s.subSlotIndex(block)
		if _, has := slotValidationBlocks[subSlot]; has {
			// the validator has already issued a block for this subslot, so we need to blacklist them.
			s.blacklist[block.ProtocolBlock().IssuerID] = struct{}{}
			// TODO: retrieve the block that has already been issued for the proof of equivocation
			// continue with enqueueing this block as we want to enqueue the first two blocks for the validator for the subslot
		}
		slotValidationBlocks[subSlot] = block
	}

	if block.SetEnqueued() {
		s.events.BlockEnqueued.Trigger(block)
	}

	if s.tryReadyValidationBlock(block) {
		s.scheduleValidationBlock(block)
	}
}

func (s *Scheduler) mainLoop() {
	var blockToSchedule *blocks.Block
loop:
	for {
		select {
		// on close, exit the loop
		case <-s.shutdownSignal:
			break loop
		// when a block is pushed by the buffer
		case blockToSchedule = <-s.blockChan:
			currentAPI := s.apiProvider.CurrentAPI()
			rate := currentAPI.ProtocolParameters().CongestionControlParameters().SchedulerRate
			tokensRequired := float64(blockToSchedule.WorkScore()) - (s.tokenBucket + float64(rate)*time.Since(s.lastScheduleTime).Seconds())
			if tokensRequired > 0 {
				// wait until sufficient tokens in token bucket
				timer := time.NewTimer(time.Duration(tokensRequired/float64(rate)) * time.Second)
				<-timer.C
			}
			s.tokenBucket = lo.Min(
				float64(currentAPI.MaxBlockWork()),
				s.tokenBucket+float64(rate)*time.Since(s.lastScheduleTime).Seconds(),
			)
			s.lastScheduleTime = time.Now()
			s.scheduleBlock(blockToSchedule)
		}
	}
}

func (s *Scheduler) scheduleBlock(block *blocks.Block) {
	if block.SetScheduled() {
		// deduct tokens from the token bucket according to the scheduled block's work.
		s.tokenBucket -= float64(block.WorkScore())

		// check for another block ready to schedule
		s.updateChildrenWithLocking(block)
		s.selectBlockToScheduleWithLocking()

		s.events.BlockScheduled.Trigger(block)
	}
}

func (s *Scheduler) scheduleValidationBlock(block *blocks.Block) {
	if block.SetScheduled() {
		s.events.BlockScheduled.Trigger(block)
		s.updateChildrenWithoutLocking(block)
	}
}

func (s *Scheduler) selectBlockToScheduleWithLocking() {
	s.bufferMutex.Lock()
	defer s.bufferMutex.Unlock()

	slotIndex := s.latestCommittedSlot()

	// already a block selected to be scheduled.
	if len(s.blockChan) > 0 {
		return
	}
	start := s.buffer.Current()
	// no blocks submitted
	if start == nil {
		return
	}

	rounds, schedulingIssuer := s.selectIssuer(start, slotIndex)

	// if there is no issuer with a ready block, we cannot schedule anything
	if schedulingIssuer == nil {
		return
	}

	if rounds > 0 {
		// increment every issuer's deficit for the required number of rounds
		for q := start; ; {
			issuerID := q.IssuerID()
			if err := s.incrementDeficit(issuerID, rounds, slotIndex); err != nil {
				s.errorHandler(ierrors.Wrapf(err, "failed to increment deficit for issuerID %s in slot %d", issuerID, slotIndex))
				s.removeIssuer(issuerID, err)

				q = s.buffer.Current()
			} else {
				q = s.buffer.Next()
			}
			if q == nil {
				return
			}
			if q == start {
				break
			}
		}
	}
	// increment the deficit for all issuers before schedulingIssuer one more time
	for q := start; q != schedulingIssuer; q = s.buffer.Next() {
		issuerID := q.IssuerID()
		if err := s.incrementDeficit(issuerID, 1, slotIndex); err != nil {
			s.errorHandler(ierrors.Wrapf(err, "failed to increment deficit for issuerID %s in slot %d", issuerID, slotIndex))
			s.removeIssuer(issuerID, err)

			return
		}
	}

	// remove the block from the buffer and adjust issuer's deficit
	block := s.buffer.PopFront()
	issuerID := block.ProtocolBlock().IssuerID
	err := s.updateDeficit(issuerID, -s.deficitFromWork(block.WorkScore()))

	if err != nil {
		// if something goes wrong with deficit update, drop the block instead of scheduling it.
		block.SetDropped()
		s.events.BlockDropped.Trigger(block, err)

		return
	}
	s.blockChan <- block
}

func (s *Scheduler) selectIssuer(start *IssuerQueue, slotIndex iotago.SlotIndex) (Deficit, *IssuerQueue) {
	rounds := Deficit(math.MaxInt64)
	var schedulingIssuer *IssuerQueue

	for q := start; ; {
		block := q.Front()
		var issuerRemoved bool

		for block != nil && time.Now().After(block.IssuingTime()) {
			currentAPI := s.apiProvider.CurrentAPI()
			if block.IsAccepted() && time.Since(block.IssuingTime()) > time.Duration(currentAPI.TimeProvider().SlotDurationSeconds()*int64(currentAPI.ProtocolParameters().MaxCommittableAge())) {
				if block.SetSkipped() {
					s.updateChildrenWithoutLocking(block)
					s.events.BlockSkipped.Trigger(block)
				}

				s.buffer.PopFront()

				block = q.Front()

				continue
			}

			issuerID := block.ProtocolBlock().IssuerID

			// compute how often the deficit needs to be incremented until the block can be scheduled
			deficit, exists := s.deficits.Get(issuerID)
			if !exists {
				panic("deficit not found for issuer")
			}

			remainingDeficit := s.deficitFromWork(block.WorkScore()) - deficit
			// calculate how many rounds we need to skip to accumulate enough deficit.
			quantum, err := s.quantumFunc(issuerID, slotIndex)
			if err != nil {
				s.errorHandler(ierrors.Wrapf(err, "failed to retrieve quantum for issuerID %s in slot %d during issuer selection", issuerID, slotIndex))
				// if quantum, can't be retrieved, we need to remove this issuer.
				s.removeIssuer(issuerID, err)
				issuerRemoved = true

				break
			}

			numerator, err := safemath.SafeAdd(remainingDeficit, quantum-1)
			if err != nil {
				numerator = math.MaxInt64
			}

			r, err := safemath.SafeDiv(numerator, quantum)
			if err != nil {
				panic(err)
			}

			// find the first issuer that will be allowed to schedule a block
			if r < rounds {
				rounds = r
				schedulingIssuer = q
			}

			break
		}

		if issuerRemoved {
			q = s.buffer.Current()
		} else {
			q = s.buffer.Next()
		}
		if q == start || q == nil {
			break
		}
	}

	return rounds, schedulingIssuer
}

func (s *Scheduler) removeIssuer(issuerID iotago.AccountID, err error) {
	q := s.buffer.IssuerQueue(issuerID)
	q.submitted.ForEach(func(id iotago.BlockID, block *blocks.Block) bool {
		block.SetDropped()
		s.events.BlockDropped.Trigger(block, err)

		return true
	})

	for q.inbox.Len() > 0 {
		block := q.PopFront()
		block.SetDropped()
		s.events.BlockDropped.Trigger(block, err)
	}

	s.deficits.Delete(issuerID)

	s.buffer.RemoveIssuer(issuerID)
}

func (s *Scheduler) createIssuer(accountID iotago.AccountID) *IssuerQueue {
	issuerQueue := s.buffer.CreateIssuerQueue(accountID)
	s.deficits.Set(accountID, 0)

	return issuerQueue
}

func (s *Scheduler) updateDeficit(accountID iotago.AccountID, delta Deficit) error {
	var updateErr error
	s.deficits.Compute(accountID, func(currentValue Deficit, exists bool) Deficit {
		if !exists {
			updateErr = ierrors.Errorf("could not get deficit for issuer %s", accountID)
			return 0
		}
		newDeficit, err := safemath.SafeAdd(currentValue, delta)
		if err != nil {
			// It can only overflow. We never allow the value to go below 0, so underflow is impossible.
			return s.maxDeficit()
		}

		// If the new deficit is negative, it could only be a result of subtraction and an error should be returned.
		if newDeficit < 0 {
			updateErr = ierrors.Errorf("deficit for issuer %s decreased below zero", accountID)
			return 0
		}

		return lo.Min(newDeficit, s.maxDeficit())
	})

	if updateErr != nil {
		s.removeIssuer(accountID, updateErr)

		return updateErr
	}

	return nil
}

func (s *Scheduler) incrementDeficit(issuerID iotago.AccountID, rounds Deficit, slotIndex iotago.SlotIndex) error {
	quantum, err := s.quantumFunc(issuerID, slotIndex)
	if err != nil {
		return err
	}

	delta, err := safemath.SafeMul(quantum, rounds)
	if err != nil {
		// overflow, set to max deficit
		delta = s.maxDeficit()
	}

	return s.updateDeficit(issuerID, delta)
}

func (s *Scheduler) isEligible(block *blocks.Block) (eligible bool) {
	return block.IsSkipped() || block.IsScheduled() || block.IsAccepted()
}

// isReady returns true if the given blockID's parents are eligible.
func (s *Scheduler) isReady(block *blocks.Block) bool {
	ready := true
	block.ForEachParent(func(parent iotago.Parent) {
		if parentBlock, parentExists := s.blockCache.Block(parent.ID); !parentExists || !s.isEligible(parentBlock) {
			// if parents are evicted and orphaned (not root blocks), or have not been received yet they will not exist.
			// if parents are evicted, they will be returned as root blocks with scheduled==true here.
			ready = false

			return
		}
	})

	return ready
}

// tryReady tries to set the given block as ready.
func (s *Scheduler) tryReady(block *blocks.Block) {
	if s.isReady(block) {
		s.ready(block)
	}
}

// tryReadyValidator tries to set the given validation block as ready.
func (s *Scheduler) tryReadyValidationBlock(block *blocks.Block) bool {
	if s.isReady(block) {
		s.readyValidationBlock(block)

		return true
	}

	return false
}

func (s *Scheduler) ready(block *blocks.Block) {
	s.buffer.Ready(block)
}

func (s *Scheduler) readyValidationBlock(block *blocks.Block) {
	s.validatorBuffer[block.ProtocolBlock().IssuerID].Unsubmit(block)
}

// updateChildrenWithLocking locks the buffer mutex and iterates over the direct children of the given blockID and
// tries to mark them as ready.
func (s *Scheduler) updateChildrenWithLocking(block *blocks.Block) {
	s.bufferMutex.Lock()
	defer s.bufferMutex.Unlock()

	s.updateChildrenWithoutLocking(block)
}

// updateChildrenWithoutLocking iterates over the direct children of the given blockID and
// tries to mark them as ready.
func (s *Scheduler) updateChildrenWithoutLocking(block *blocks.Block) {
	for _, childBlock := range block.Children() {
		if _, childBlockExists := s.blockCache.Block(childBlock.ID()); childBlockExists && childBlock.IsEnqueued() {
			if _, isBasic := childBlock.BasicBlock(); isBasic {
				s.tryReady(childBlock)
			} else if _, isValidation := childBlock.ValidationBlock(); isValidation {
				if s.tryReadyValidationBlock(childBlock) {
					s.scheduleValidationBlock(childBlock)
				}
			} else {
				panic("invalid block type")
			}
		}
	}
}

func (s *Scheduler) maxDeficit() Deficit {
	return Deficit(math.MaxInt64 / 2)
}

func (s *Scheduler) deficitFromWork(work iotago.WorkScore) Deficit {
	// max workscore block should occupy the full range of the deficit
	deficitScaleFactor := s.maxDeficit() / Deficit(s.apiProvider.CurrentAPI().MaxBlockWork())
	return Deficit(work) * deficitScaleFactor
}
