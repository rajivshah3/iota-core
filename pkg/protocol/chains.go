package protocol

import (
	"github.com/iotaledger/hive.go/core/eventticker"
	"github.com/iotaledger/hive.go/ds/reactive"
	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/core/promise"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/network"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Chains struct {
	mainChain reactive.Variable[*Chain]

	commitments *shrinkingmap.ShrinkingMap[iotago.CommitmentID, *promise.Promise[*Commitment]]

	commitmentCreated *event.Event1[*Commitment]

	chainCreated *event.Event1[*Chain]

	CommitmentRequester *eventticker.EventTicker[iotago.SlotIndex, iotago.CommitmentID]

	*ChainSwitching

	*AttestationsRequester

	reactive.EvictionState[iotago.SlotIndex]
}

func newChains(protocol *Protocol) *Chains {
	c := &Chains{
		EvictionState: reactive.NewEvictionState[iotago.SlotIndex](),
		mainChain: reactive.NewVariable[*Chain]().Init(
			NewChain(NewCommitment(protocol.MainEngine().LatestCommitment(), true), protocol.MainEngine()),
		),
		commitments:         shrinkingmap.New[iotago.CommitmentID, *promise.Promise[*Commitment]](),
		commitmentCreated:   event.New1[*Commitment](),
		chainCreated:        event.New1[*Chain](),
		CommitmentRequester: eventticker.New[iotago.SlotIndex, iotago.CommitmentID](),
	}

	// embed reactive orchestrators
	c.ChainSwitching = NewChainSwitching(c)
	c.AttestationsRequester = NewAttestationsRequester(protocol)

	return c
}

func (c *Chains) ProcessCommitment(commitment *model.Commitment) (commitmentMetadata *Commitment) {
	if commitmentRequest := c.requestCommitment(commitment.ID(), commitment.Index(), false, func(resolvedMetadata *Commitment) {
		commitmentMetadata = resolvedMetadata
	}); commitmentRequest != nil {
		commitmentRequest.Resolve(NewCommitment(commitment))
	}

	return commitmentMetadata
}

func (c *Chains) Commitment(commitmentID iotago.CommitmentID) (commitment *Commitment, exists bool) {
	commitmentRequest, exists := c.commitments.Get(commitmentID)
	if !exists || !commitmentRequest.WasCompleted() {
		return nil, false
	}

	commitmentRequest.OnSuccess(func(result *Commitment) {
		commitment = result
	})

	return commitment, true
}

func (c *Chains) OnCommitmentRequested(callback func(commitmentID iotago.CommitmentID)) (unsubscribe func()) {
	return c.CommitmentRequester.Events.TickerStarted.Hook(callback).Unhook
}

func (c *Chains) OnCommitmentCreated(callback func(commitment *Commitment)) (unsubscribe func()) {
	return c.commitmentCreated.Hook(callback).Unhook
}

func (c *Chains) OnChainCreated(callback func(chain *Chain)) (unsubscribe func()) {
	return c.chainCreated.Hook(callback).Unhook
}

func (c *Chains) MainChain() *Chain {
	return c.mainChain.Get()
}

func (c *Chains) MainChainR() reactive.Variable[*Chain] {
	return c.mainChain
}

func (c *Chains) ProcessSlotCommitmentRequest(commitmentID iotago.CommitmentID, src network.PeerID) {
	if commitment, exists := c.protocol.Commitment(commitmentID); !exists {
		c.protocol.SendSlotCommitment(commitment.CommitmentModel(), src)
	}
}

func (c *Chains) setupCommitment(commitment *Commitment, slotEvictedEvent reactive.Event) {
	c.requestCommitment(commitment.PrevID(), commitment.Index()-1, true, commitment.setParent)

	slotEvictedEvent.OnTrigger(func() {
		commitment.evicted.Trigger()
	})

	c.commitmentCreated.Trigger(commitment)

	commitment.spawnedChain.OnUpdate(func(_, newChain *Chain) {
		if newChain != nil {
			c.chainCreated.Trigger(newChain)
		}
	})
}

func (c *Chains) requestCommitment(commitmentID iotago.CommitmentID, index iotago.SlotIndex, requestFromPeers bool, optSuccessCallbacks ...func(metadata *Commitment)) (commitmentRequest *promise.Promise[*Commitment]) {
	slotEvicted := c.EvictionEvent(index)
	if slotEvicted.WasTriggered() {
		rootCommitment := c.mainChain.Get().Root()

		if rootCommitment == nil || commitmentID != rootCommitment.ID() {
			return nil
		}

		for _, successCallback := range optSuccessCallbacks {
			successCallback(rootCommitment)
		}

		return promise.New[*Commitment]().Resolve(rootCommitment)
	}

	commitmentRequest, requestCreated := c.commitments.GetOrCreate(commitmentID, lo.NoVariadic(promise.New[*Commitment]))
	if requestCreated {
		if requestFromPeers {
			c.CommitmentRequester.StartTicker(commitmentID)

			commitmentRequest.OnComplete(func() {
				c.CommitmentRequester.StopTicker(commitmentID)
			})
		}

		commitmentRequest.OnSuccess(func(commitment *Commitment) {
			c.setupCommitment(commitment, slotEvicted)
		})

		slotEvicted.OnTrigger(func() { c.commitments.Delete(commitmentID) })
	}

	for _, successCallback := range optSuccessCallbacks {
		commitmentRequest.OnSuccess(successCallback)
	}

	return commitmentRequest
}
