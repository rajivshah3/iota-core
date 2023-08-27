package chainmanagerv1

import (
	"github.com/iotaledger/hive.go/core/eventticker"
	"github.com/iotaledger/hive.go/ds/reactive"
	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/core/promise"
	"github.com/iotaledger/iota-core/pkg/model"
	iotago "github.com/iotaledger/iota.go/v4"
)

type ChainManager struct {
	mainChain reactive.Variable[*Chain]

	heaviestClaimedCandidate reactive.Variable[*Chain]

	heaviestAttestedCandidate reactive.Variable[*Chain]

	heaviestVerifiedCandidate reactive.Variable[*Chain]

	commitments *shrinkingmap.ShrinkingMap[iotago.CommitmentID, *promise.Promise[*CommitmentMetadata]]

	commitmentRequester *eventticker.EventTicker[iotago.SlotIndex, iotago.CommitmentID]

	commitmentCreated *event.Event1[*CommitmentMetadata]

	chainCreated *event.Event1[*Chain]

	reactive.EvictionState[iotago.SlotIndex]
}

func NewChainManager(rootCommitment *model.Commitment) *ChainManager {
	c := &ChainManager{
		mainChain:                 reactive.NewVariable[*Chain]().Init(NewChain(NewRootCommitmentMetadata(rootCommitment))),
		heaviestClaimedCandidate:  reactive.NewVariable[*Chain](),
		heaviestAttestedCandidate: reactive.NewVariable[*Chain](),
		heaviestVerifiedCandidate: reactive.NewVariable[*Chain](),
		commitments:               shrinkingmap.New[iotago.CommitmentID, *promise.Promise[*CommitmentMetadata]](),
		commitmentRequester:       eventticker.New[iotago.SlotIndex, iotago.CommitmentID](),
		commitmentCreated:         event.New1[*CommitmentMetadata](),
		chainCreated:              event.New1[*Chain](),
		EvictionState:             reactive.NewEvictionState[iotago.SlotIndex](),
	}

	c.OnChainCreated(func(chain *Chain) {
		c.selectHeaviestCandidate(c.heaviestClaimedCandidate, chain, (*ChainWeight).ReactiveClaimed)
		c.selectHeaviestCandidate(c.heaviestAttestedCandidate, chain, (*ChainWeight).ReactiveAttested)
		c.selectHeaviestCandidate(c.heaviestVerifiedCandidate, chain, (*ChainWeight).ReactiveVerified)
	})

	return c
}

func (c *ChainManager) ProcessCommitment(commitment *model.Commitment) (commitmentMetadata *CommitmentMetadata) {
	if commitmentRequest := c.requestCommitment(commitment.ID(), commitment.Index(), false, func(resolvedMetadata *CommitmentMetadata) {
		commitmentMetadata = resolvedMetadata
	}); commitmentRequest != nil {
		commitmentRequest.Resolve(NewCommitmentMetadata(commitment))
	}

	return commitmentMetadata
}

func (c *ChainManager) OnCommitmentCreated(callback func(commitment *CommitmentMetadata)) (unsubscribe func()) {
	return c.commitmentCreated.Hook(callback).Unhook
}

func (c *ChainManager) OnChainCreated(callback func(chain *Chain)) (unsubscribe func()) {
	return c.chainCreated.Hook(callback).Unhook
}

func (c *ChainManager) MainChain() *Chain {
	return c.mainChain.Get()
}

func (c *ChainManager) MainChainVar() reactive.Variable[*Chain] {
	return c.mainChain
}

func (c *ChainManager) HeaviestCandidateChain() reactive.Variable[*Chain] {
	return c.heaviestClaimedCandidate
}

func (c *ChainManager) HeaviestAttestedCandidateChain() reactive.Variable[*Chain] {
	return c.heaviestAttestedCandidate
}

func (c *ChainManager) HeaviestVerifiedCandidateChain() reactive.Variable[*Chain] {
	return c.heaviestVerifiedCandidate
}

func (c *ChainManager) RootCommitment() reactive.Variable[*CommitmentMetadata] {
	if chain := c.mainChain.Get(); chain != nil {
		return chain.ReactiveRoot()
	}

	panic("root chain not initialized")
}

func (c *ChainManager) selectHeaviestCandidate(variable reactive.Variable[*Chain], newCandidate *Chain, chainWeight func(*ChainWeight) reactive.Variable[uint64]) {
	chainWeight(newCandidate.Weight()).OnUpdate(func(_, newChainWeight uint64) {
		if newChainWeight <= c.MainChain().Weight().Verified() {
			return
		}

		variable.Compute(func(currentCandidate *Chain) *Chain {
			if currentCandidate == nil || currentCandidate.evicted.WasTriggered() || newChainWeight > chainWeight(currentCandidate.Weight()).Get() {
				return newCandidate
			}

			return currentCandidate
		})
	})
}

func (c *ChainManager) setupCommitment(commitment *CommitmentMetadata, slotEvictedEvent reactive.Event) {
	c.requestCommitment(commitment.PrevID(), commitment.Index()-1, true, lo.Void(commitment.ReactiveParent().Set))

	slotEvictedEvent.OnTrigger(func() {
		commitment.Evicted().Trigger()
	})

	c.commitmentCreated.Trigger(commitment)

	commitment.spawnedChain.OnUpdate(func(_, newChain *Chain) {
		if newChain != nil {
			c.chainCreated.Trigger(newChain)
		}
	})
}

func (c *ChainManager) requestCommitment(commitmentID iotago.CommitmentID, index iotago.SlotIndex, requestFromPeers bool, optSuccessCallbacks ...func(metadata *CommitmentMetadata)) (commitmentRequest *promise.Promise[*CommitmentMetadata]) {
	slotEvicted := c.EvictionEvent(index)
	if slotEvicted.WasTriggered() {
		rootCommitment := c.mainChain.Get().root.Get()

		if rootCommitment == nil || commitmentID != rootCommitment.ID() {
			return nil
		}

		for _, successCallback := range optSuccessCallbacks {
			successCallback(rootCommitment)
		}

		return promise.New[*CommitmentMetadata]().Resolve(rootCommitment)
	}

	commitmentRequest, requestCreated := c.commitments.GetOrCreate(commitmentID, lo.NoVariadic(promise.New[*CommitmentMetadata]))
	if requestCreated {
		if requestFromPeers {
			c.commitmentRequester.StartTicker(commitmentID)

			commitmentRequest.OnComplete(func() {
				c.commitmentRequester.StopTicker(commitmentID)
			})
		}

		commitmentRequest.OnSuccess(func(commitment *CommitmentMetadata) {
			c.setupCommitment(commitment, slotEvicted)
		})

		slotEvicted.OnTrigger(func() { c.commitments.Delete(commitmentID) })
	}

	for _, successCallback := range optSuccessCallbacks {
		commitmentRequest.OnSuccess(successCallback)
	}

	return commitmentRequest
}
