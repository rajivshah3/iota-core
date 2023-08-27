package chainmanagerv1

import (
	"github.com/iotaledger/hive.go/ds/reactive"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/iota-core/pkg/model"
	iotago "github.com/iotaledger/iota.go/v4"
)

type CommitmentMetadata struct {
	*model.Commitment

	chain                                 reactive.Variable[*Chain]
	parent                                reactive.Variable[*CommitmentMetadata]
	successor                             reactive.Variable[*CommitmentMetadata]
	solid                                 reactive.Event
	attested                              reactive.Event
	verified                              reactive.Event
	parentVerified                        reactive.Event
	belowSyncThreshold                    reactive.Event
	belowWarpSyncThreshold                reactive.Event
	belowLatestVerifiedCommitment         reactive.Event
	evicted                               reactive.Event
	parentAboveLatestVerifiedCommitment   reactive.Variable[bool]
	directlyAboveLatestVerifiedCommitment reactive.Variable[bool]
	aboveLatestVerifiedCommitment         reactive.Variable[bool]
	inSyncWindow                          reactive.Variable[bool]
	requiresWarpSync                      reactive.Variable[bool]
	spawnedChain                          reactive.Variable[*Chain]
}

func NewCommitmentMetadata(commitment *model.Commitment) *CommitmentMetadata {
	c := &CommitmentMetadata{
		Commitment: commitment,

		chain:                               reactive.NewVariable[*Chain](),
		parent:                              reactive.NewVariable[*CommitmentMetadata](),
		successor:                           reactive.NewVariable[*CommitmentMetadata](),
		solid:                               reactive.NewEvent(),
		attested:                            reactive.NewEvent(),
		verified:                            reactive.NewEvent(),
		parentVerified:                      reactive.NewEvent(),
		belowSyncThreshold:                  reactive.NewEvent(),
		belowWarpSyncThreshold:              reactive.NewEvent(),
		belowLatestVerifiedCommitment:       reactive.NewEvent(),
		evicted:                             reactive.NewEvent(),
		parentAboveLatestVerifiedCommitment: reactive.NewVariable[bool](),
		spawnedChain:                        reactive.NewVariable[*Chain](),
	}

	c.chain.OnUpdate(func(_, chain *Chain) { chain.Commitments().Register(c) })

	c.parent.OnUpdate(func(_, parent *CommitmentMetadata) {
		parent.registerChild(c, c.inheritChain(parent))

		c.registerParent(parent)
	})

	c.directlyAboveLatestVerifiedCommitment = reactive.NewDerivedVariable2(func(parentVerified, verified bool) bool {
		return parentVerified && !verified
	}, c.parentVerified, c.verified)

	c.aboveLatestVerifiedCommitment = reactive.NewDerivedVariable2(func(directlyAboveLatestVerifiedCommitment, parentAboveLatestVerifiedCommitment bool) bool {
		return directlyAboveLatestVerifiedCommitment || parentAboveLatestVerifiedCommitment
	}, c.directlyAboveLatestVerifiedCommitment, c.parentAboveLatestVerifiedCommitment)

	c.inSyncWindow = reactive.NewDerivedVariable2(func(belowSyncThreshold, aboveLatestVerifiedCommitment bool) bool {
		return belowSyncThreshold && aboveLatestVerifiedCommitment
	}, c.belowSyncThreshold, c.aboveLatestVerifiedCommitment)

	c.requiresWarpSync = reactive.NewDerivedVariable2(func(inSyncWindow, belowWarpSyncThreshold bool) bool {
		return inSyncWindow && belowWarpSyncThreshold
	}, c.inSyncWindow, c.belowWarpSyncThreshold)

	return c
}

func NewRootCommitmentMetadata(commitment *model.Commitment) *CommitmentMetadata {
	commitmentMetadata := NewCommitmentMetadata(commitment)
	commitmentMetadata.Solid().Trigger()
	commitmentMetadata.Verified().Trigger()
	commitmentMetadata.BelowSyncThreshold().Trigger()
	commitmentMetadata.BelowWarpSyncThreshold().Trigger()
	commitmentMetadata.BelowLatestVerifiedCommitment().Trigger()
	commitmentMetadata.Evicted().Trigger()

	return commitmentMetadata
}

func (c *CommitmentMetadata) Chain() reactive.Variable[*Chain] {
	return c.chain
}

func (c *CommitmentMetadata) Parent() *CommitmentMetadata {
	return c.parent.Get()
}

func (c *CommitmentMetadata) ReactiveParent() reactive.Variable[*CommitmentMetadata] {
	return c.parent
}

func (c *CommitmentMetadata) Solid() reactive.Event {
	return c.solid
}

func (c *CommitmentMetadata) Attested() reactive.Event {
	return c.attested
}

func (c *CommitmentMetadata) Verified() reactive.Event {
	return c.verified
}

func (c *CommitmentMetadata) ParentVerified() reactive.Event {
	return c.parentVerified
}

func (c *CommitmentMetadata) BelowSyncThreshold() reactive.Event {
	return c.belowSyncThreshold
}

func (c *CommitmentMetadata) BelowWarpSyncThreshold() reactive.Event {
	return c.belowWarpSyncThreshold
}

func (c *CommitmentMetadata) BelowLatestVerifiedCommitment() reactive.Event {
	return c.belowLatestVerifiedCommitment
}

func (c *CommitmentMetadata) Evicted() reactive.Event {
	return c.evicted
}

func (c *CommitmentMetadata) ParentAboveLatestVerifiedCommitment() reactive.Variable[bool] {
	return c.parentAboveLatestVerifiedCommitment
}

func (c *CommitmentMetadata) AboveLatestVerifiedCommitment() reactive.Variable[bool] {
	return c.aboveLatestVerifiedCommitment
}

func (c *CommitmentMetadata) InSyncWindow() reactive.Variable[bool] {
	return c.inSyncWindow
}

func (c *CommitmentMetadata) RequiresWarpSync() reactive.Variable[bool] {
	return c.requiresWarpSync
}

func (c *CommitmentMetadata) ChainSuccessor() reactive.Variable[*CommitmentMetadata] {
	return c.successor
}

// Max compares the given commitment with the current one and returns the one with the higher index.
func (c *CommitmentMetadata) Max(latestCommitment *CommitmentMetadata) *CommitmentMetadata {
	if c == nil || latestCommitment != nil && latestCommitment.Index() >= c.Index() {
		return latestCommitment
	}

	return c
}

func (c *CommitmentMetadata) registerParent(parent *CommitmentMetadata) {
	c.solid.InheritFrom(parent.solid)
	c.parentVerified.InheritFrom(parent.verified)
	c.parentAboveLatestVerifiedCommitment.InheritFrom(parent.aboveLatestVerifiedCommitment)

	// triggerIfBelowThreshold triggers the given event if the commitment's index is below the given
	// threshold. We only monitor the threshold after the corresponding parent event was triggered (to minimize
	// the amount of elements that listen to updates of the same chain threshold - it spreads monotonically).
	triggerIfBelowThreshold := func(event func(*CommitmentMetadata) reactive.Event, chainThreshold func(*ChainThresholds) reactive.Variable[iotago.SlotIndex]) {
		event(parent).OnTrigger(func() {
			chainThreshold(c.chain.Get().Thresholds()).OnUpdateOnce(func(_, _ iotago.SlotIndex) {
				event(c).Trigger()
			}, func(_ iotago.SlotIndex, slotIndex iotago.SlotIndex) bool {
				return c.Index() < slotIndex
			})
		})
	}

	triggerIfBelowThreshold((*CommitmentMetadata).BelowLatestVerifiedCommitment, (*ChainThresholds).ReactiveLatestVerifiedIndex)
	triggerIfBelowThreshold((*CommitmentMetadata).BelowSyncThreshold, (*ChainThresholds).ReactiveSyncThreshold)
	triggerIfBelowThreshold((*CommitmentMetadata).BelowWarpSyncThreshold, (*ChainThresholds).ReactiveWarpSyncThreshold)
}

func (c *CommitmentMetadata) registerChild(newChild *CommitmentMetadata, onSuccessorUpdated func(*CommitmentMetadata, *CommitmentMetadata)) {
	c.successor.Compute(func(currentSuccessor *CommitmentMetadata) *CommitmentMetadata {
		return lo.Cond(currentSuccessor != nil, currentSuccessor, newChild)
	})

	// unsubscribe the handler on eviction to prevent memory leaks
	c.evicted.OnTrigger(c.successor.OnUpdate(onSuccessorUpdated))
}

func (c *CommitmentMetadata) inheritChain(parent *CommitmentMetadata) func(*CommitmentMetadata, *CommitmentMetadata) {
	var unsubscribeFromParent func()

	return func(_, successor *CommitmentMetadata) {
		c.spawnedChain.Compute(func(spawnedChain *Chain) (newSpawnedChain *Chain) {
			switch successor {
			case nil:
				panic("successor may not be changed back to nil")
			case c:
				if spawnedChain != nil {
					spawnedChain.evicted.Trigger()
				}

				unsubscribeFromParent = parent.chain.OnUpdate(func(_, chain *Chain) { c.chain.Set(chain) })
			default:
				if spawnedChain != nil {
					return spawnedChain
				}

				if unsubscribeFromParent != nil {
					unsubscribeFromParent()
				}

				newSpawnedChain = NewChain(c)

				c.chain.Set(newSpawnedChain)
			}

			return newSpawnedChain
		})
	}
}
