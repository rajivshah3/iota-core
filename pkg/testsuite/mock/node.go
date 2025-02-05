package mock

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"

	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/hive.go/runtime/workerpool"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/pkg/protocol/chainmanager"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/commitmentfilter"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/filter"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/notarization"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/merklehasher"
)

// idAliases contains a list of aliases registered for a set of IDs.
var idAliases = make(map[peer.ID]string)

// RegisterIDAlias registers an alias that will modify the String() output of the ID to show a human
// readable string instead of the base58 encoded version of itself.
func RegisterIDAlias(id peer.ID, alias string) {
	idAliases[id] = alias
}

// UnregisterIDAliases removes all aliases registered through the RegisterIDAlias function.
func UnregisterIDAliases() {
	idAliases = make(map[peer.ID]string)
}

type Node struct {
	Testing *testing.T

	Name      string
	Validator *BlockIssuer

	ctx       context.Context
	ctxCancel context.CancelFunc

	PeerID                  peer.ID
	protocolParametersHash  iotago.Identifier
	highestSupportedVersion iotago.Version

	Partition string
	Endpoint  *Endpoint
	Workers   *workerpool.Group

	Protocol *protocol.Protocol

	forkDetectedCount             atomic.Uint32
	candidateEngineActivatedCount atomic.Uint32
	mainEngineSwitchedCount       atomic.Uint32

	mutex          syncutils.RWMutex
	attachedBlocks []*blocks.Block
}

func NewNode(t *testing.T, net *Network, partition string, name string, validator bool) *Node {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}

	accountID := iotago.AccountID(blake2b.Sum256(pub))
	accountID.RegisterAlias(name)

	peerID := lo.PanicOnErr(peer.IDFromPrivateKey(lo.PanicOnErr(p2pcrypto.UnmarshalEd25519PrivateKey(priv))))
	RegisterIDAlias(peerID, name)

	var validatorBlockIssuer *BlockIssuer
	if validator {
		validatorBlockIssuer = NewBlockIssuer(t, name, validator)
	} else {
		validatorBlockIssuer = nil
	}

	return &Node{
		Testing: t,

		Name: name,

		Validator: validatorBlockIssuer,

		PeerID: peerID,

		Partition: partition,
		Endpoint:  net.JoinWithEndpointID(peerID, partition),
		Workers:   workerpool.NewGroup(name),

		attachedBlocks: make([]*blocks.Block, 0),
	}
}

func (n *Node) IsValidator() bool {
	return n.Validator != nil
}

func (n *Node) Initialize(failOnBlockFiltered bool, opts ...options.Option[protocol.Protocol]) {
	n.Protocol = protocol.New(n.Workers.CreateGroup("Protocol"),
		n.Endpoint,
		opts...,
	)

	n.hookEvents()
	n.hookLogging(failOnBlockFiltered)

	n.ctx, n.ctxCancel = context.WithCancel(context.Background())

	started := make(chan struct{}, 1)

	n.Protocol.HookInitialized(func() {
		close(started)
	})

	go func() {
		defer n.ctxCancel()

		if err := n.Protocol.Run(n.ctx); err != nil {
			fmt.Printf("%s > Run finished with error: %s\n", n.Name, err.Error())
		}
	}()

	<-started
}

func (n *Node) hookEvents() {
	events := n.Protocol.Events

	events.ChainManager.ForkDetected.Hook(func(fork *chainmanager.Fork) { n.forkDetectedCount.Add(1) })

	events.CandidateEngineActivated.Hook(func(e *engine.Engine) { n.candidateEngineActivatedCount.Add(1) })

	events.MainEngineSwitched.Hook(func(e *engine.Engine) { n.mainEngineSwitchedCount.Add(1) })
}

func (n *Node) hookLogging(failOnBlockFiltered bool) {
	events := n.Protocol.Events

	n.attachEngineLogs(failOnBlockFiltered, n.Protocol.MainEngineInstance())

	events.Network.BlockReceived.Hook(func(block *model.Block, source peer.ID) {
		fmt.Printf("%s > Network.BlockReceived: from %s %s - %d\n", n.Name, source, block.ID(), block.ID().Slot())
	})

	events.Network.BlockRequestReceived.Hook(func(blockID iotago.BlockID, source peer.ID) {
		fmt.Printf("%s > Network.BlockRequestReceived: from %s %s\n", n.Name, source, blockID)
	})

	events.Network.SlotCommitmentReceived.Hook(func(commitment *model.Commitment, source peer.ID) {
		fmt.Printf("%s > Network.SlotCommitmentReceived: from %s %s\n", n.Name, source, commitment.ID())
	})

	events.Network.SlotCommitmentRequestReceived.Hook(func(commitmentID iotago.CommitmentID, source peer.ID) {
		fmt.Printf("%s > Network.SlotCommitmentRequestReceived: from %s %s\n", n.Name, source, commitmentID)
	})

	events.Network.AttestationsReceived.Hook(func(commitment *model.Commitment, attestations []*iotago.Attestation, merkleProof *merklehasher.Proof[iotago.Identifier], source peer.ID) {
		fmt.Printf("%s > Network.AttestationsReceived: from %s %s number of attestations: %d with merkleProof: %s - %s\n", n.Name, source, commitment.ID(), len(attestations), lo.PanicOnErr(merkleProof.JSONEncode()), lo.Map(attestations, func(a *iotago.Attestation) iotago.BlockID {
			return lo.PanicOnErr(a.BlockID())
		}))
	})

	events.Network.AttestationsRequestReceived.Hook(func(id iotago.CommitmentID, source peer.ID) {
		fmt.Printf("%s > Network.AttestationsRequestReceived: from %s %s\n", n.Name, source, id)
	})

	events.Network.WarpSyncResponseReceived.Hook(func(id iotago.CommitmentID, ds iotago.BlockIDs, m *merklehasher.Proof[iotago.Identifier], ds2 iotago.TransactionIDs, m2 *merklehasher.Proof[iotago.Identifier], id2 peer.ID) {
		fmt.Printf("%s > Network.WarpSyncResponseReceived: from %s %s\n", n.Name, id2, id)
	})

	events.Network.WarpSyncRequestReceived.Hook(func(id iotago.CommitmentID, id2 peer.ID) {
		fmt.Printf("%s > Network.WarpSyncRequestReceived: from %s %s\n", n.Name, id2, id)
	})

	// events.ChainManager.CommitmentBelowRoot.Hook(func(commitmentID iotago.CommitmentID) {
	//	fmt.Printf("%s > ChainManager.CommitmentBelowRoot: %s\n", n.Name, commitmentID)
	// })

	events.ChainManager.ForkDetected.Hook(func(fork *chainmanager.Fork) {
		fmt.Printf("%s > ChainManager.ForkDetected: %s\n", n.Name, fork)
	})

	// events.Engine.TipManager.BlockAdded.Hook(func(tipMetadata tipmanager.TipMetadata) {
	//	fmt.Printf("%s > TipManager.BlockAdded: %s in pool %d\n", n.Name, tipMetadata.ID(), tipMetadata.TipPool().Get())
	// })

	events.CandidateEngineActivated.Hook(func(e *engine.Engine) {
		fmt.Printf("%s > CandidateEngineActivated: %s, ChainID:%s Slot:%s\n", n.Name, e.Name(), e.ChainID(), e.ChainID().Slot())

		n.attachEngineLogs(failOnBlockFiltered, e)
	})

	events.MainEngineSwitched.Hook(func(e *engine.Engine) {
		fmt.Printf("%s > MainEngineSwitched: %s, ChainID:%s Slot:%s\n", n.Name, e.Name(), e.ChainID(), e.ChainID().Slot())
	})

	events.MainEngineRestarted.Hook(func(e *engine.Engine) {
		fmt.Printf("%s > MainEngineRestarted: %s, ChainID:%s Slot:%s\n", n.Name, e.Name(), e.ChainID(), e.ChainID().Slot())

		n.attachEngineLogsWithName(failOnBlockFiltered, e, fmt.Sprintf("Main2 - %s", e.Name()[:8]))
	})

	events.Network.Error.Hook(func(err error, id peer.ID) {
		fmt.Printf("%s > Network.Error: from %s %s\n", n.Name, id, err)
	})

	events.Error.Hook(func(err error) {
		fmt.Printf("%s > Protocol.Error: %s\n", n.Name, err.Error())
	})
}

func (n *Node) attachEngineLogsWithName(failOnBlockFiltered bool, instance *engine.Engine, engineName string) {
	events := instance.Events

	events.BlockDAG.BlockAttached.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] BlockDAG.BlockAttached: %s\n", n.Name, engineName, block.ID())

		n.mutex.Lock()
		defer n.mutex.Unlock()
		n.attachedBlocks = append(n.attachedBlocks, block)
	})

	events.BlockDAG.BlockSolid.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] BlockDAG.BlockSolid: %s\n", n.Name, engineName, block.ID())
	})

	events.BlockDAG.BlockInvalid.Hook(func(block *blocks.Block, err error) {
		fmt.Printf("%s > [%s] BlockDAG.BlockInvalid: %s - %s\n", n.Name, engineName, block.ID(), err)
	})

	events.BlockDAG.BlockMissing.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] BlockDAG.BlockMissing: %s\n", n.Name, engineName, block.ID())
	})

	events.BlockDAG.MissingBlockAttached.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] BlockDAG.MissingBlockAttached: %s\n", n.Name, engineName, block.ID())
	})

	events.SeatManager.BlockProcessed.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] SybilProtection.BlockProcessed: %s\n", n.Name, engineName, block.ID())
	})

	events.Booker.BlockBooked.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] Booker.BlockBooked: %s\n", n.Name, engineName, block.ID())
	})

	events.Booker.BlockInvalid.Hook(func(block *blocks.Block, err error) {
		fmt.Printf("%s > [%s] Booker.BlockInvalid: %s - %s\n", n.Name, engineName, block.ID(), err.Error())
	})

	events.Booker.TransactionInvalid.Hook(func(metadata mempool.TransactionMetadata, err error) {
		fmt.Printf("%s > [%s] Booker.TransactionInvalid: %s - %s\n", n.Name, engineName, metadata.ID(), err.Error())
	})

	events.Scheduler.BlockScheduled.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] Scheduler.BlockScheduled: %s\n", n.Name, engineName, block.ID())
	})

	events.Scheduler.BlockEnqueued.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] Scheduler.BlockEnqueued: %s\n", n.Name, engineName, block.ID())
	})

	events.Scheduler.BlockSkipped.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] Scheduler.BlockSkipped: %s\n", n.Name, engineName, block.ID())
	})

	events.Scheduler.BlockDropped.Hook(func(block *blocks.Block, err error) {
		fmt.Printf("%s > [%s] Scheduler.BlockDropped: %s - %s\n", n.Name, engineName, block.ID(), err.Error())
	})

	events.Clock.AcceptedTimeUpdated.Hook(func(newTime time.Time) {
		fmt.Printf("%s > [%s] Clock.AcceptedTimeUpdated: %s [Slot %d]\n", n.Name, engineName, newTime, instance.LatestAPI().TimeProvider().SlotFromTime(newTime))
	})

	events.Clock.ConfirmedTimeUpdated.Hook(func(newTime time.Time) {
		fmt.Printf("%s > [%s] Clock.ConfirmedTimeUpdated: %s [Slot %d]\n", n.Name, engineName, newTime, instance.LatestAPI().TimeProvider().SlotFromTime(newTime))
	})

	events.Filter.BlockPreAllowed.Hook(func(block *model.Block) {
		fmt.Printf("%s > [%s] Filter.BlockPreAllowed: %s\n", n.Name, engineName, block.ID())
	})

	events.Filter.BlockPreFiltered.Hook(func(event *filter.BlockPreFilteredEvent) {
		fmt.Printf("%s > [%s] Filter.BlockPreFiltered: %s - %s\n", n.Name, engineName, event.Block.ID(), event.Reason.Error())
		if failOnBlockFiltered {
			n.Testing.Fatal("no blocks should be prefiltered")
		}
	})

	events.CommitmentFilter.BlockAllowed.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] CommitmentFilter.BlockAllowed: %s\n", n.Name, engineName, block.ID())
	})

	events.CommitmentFilter.BlockFiltered.Hook(func(event *commitmentfilter.BlockFilteredEvent) {
		fmt.Printf("%s > [%s] CommitmentFilter.BlockFiltered: %s - %s\n", n.Name, engineName, event.Block.ID(), event.Reason.Error())
		if failOnBlockFiltered {
			n.Testing.Fatal("no blocks should be filtered")
		}
	})

	events.BlockRequester.Tick.Hook(func(blockID iotago.BlockID) {
		fmt.Printf("%s > [%s] BlockRequester.Tick: %s\n", n.Name, engineName, blockID)
	})

	events.BlockProcessed.Hook(func(blockID iotago.BlockID) {
		fmt.Printf("%s > [%s] Engine.BlockProcessed: %s\n", n.Name, engineName, blockID)
	})

	events.Notarization.SlotCommitted.Hook(func(details *notarization.SlotCommittedDetails) {
		var acceptedBlocks iotago.BlockIDs
		err := details.AcceptedBlocks.Stream(func(id iotago.BlockID) error {
			acceptedBlocks = append(acceptedBlocks, id)
			return nil
		})
		require.NoError(n.Testing, err)

		rootsStorage, err := instance.Storage.Roots(details.Commitment.ID().Slot())
		require.NoError(n.Testing, err, "roots storage for slot %d not found", details.Commitment.Slot())
		roots, err := rootsStorage.Load(details.Commitment.ID())
		require.NoError(n.Testing, err)

		attestationBlockIDs := make([]iotago.BlockID, 0)
		tree, err := instance.Attestations.GetMap(details.Commitment.Slot())
		if err == nil {
			err = tree.Stream(func(key iotago.AccountID, value *iotago.Attestation) error {
				attestationBlockIDs = append(attestationBlockIDs, lo.PanicOnErr(value.BlockID()))
				return nil
			})
			require.NoError(n.Testing, err)
		}

		fmt.Printf("%s > [%s] NotarizationManager.SlotCommitted: %s %s Accepted Blocks: %s\n %s\n Attestations: %s\n", n.Name, engineName, details.Commitment.ID(), details.Commitment, acceptedBlocks, roots, attestationBlockIDs)
	})

	events.Notarization.LatestCommitmentUpdated.Hook(func(commitment *model.Commitment) {
		fmt.Printf("%s > [%s] NotarizationManager.LatestCommitmentUpdated: %s\n", n.Name, engineName, commitment.ID())
	})

	events.BlockGadget.BlockPreAccepted.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] Consensus.BlockGadget.BlockPreAccepted: %s %s\n", n.Name, engineName, block.ID(), block.ProtocolBlock().SlotCommitmentID)
	})

	events.BlockGadget.BlockAccepted.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] Consensus.BlockGadget.BlockAccepted: %s @ slot %s committing to %s\n", n.Name, engineName, block.ID(), block.ID().Slot(), block.ProtocolBlock().SlotCommitmentID)
	})

	events.BlockGadget.BlockPreConfirmed.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] Consensus.BlockGadget.BlockPreConfirmed: %s %s\n", n.Name, engineName, block.ID(), block.ProtocolBlock().SlotCommitmentID)
	})

	events.BlockGadget.BlockConfirmed.Hook(func(block *blocks.Block) {
		fmt.Printf("%s > [%s] Consensus.BlockGadget.BlockConfirmed: %s %s\n", n.Name, engineName, block.ID(), block.ProtocolBlock().SlotCommitmentID)
	})

	events.SlotGadget.SlotFinalized.Hook(func(slot iotago.SlotIndex) {
		fmt.Printf("%s > [%s] Consensus.SlotGadget.SlotFinalized: %s\n", n.Name, engineName, slot)
	})

	events.SeatManager.OnlineCommitteeSeatAdded.Hook(func(seat account.SeatIndex, accountID iotago.AccountID) {
		fmt.Printf("%s > [%s] SybilProtection.OnlineCommitteeSeatAdded: %d - %s\n", n.Name, engineName, seat, accountID)
	})

	events.SeatManager.OnlineCommitteeSeatRemoved.Hook(func(seat account.SeatIndex) {
		fmt.Printf("%s > [%s] SybilProtection.OnlineCommitteeSeatRemoved: %d\n", n.Name, engineName, seat)
	})

	events.ConflictDAG.ConflictCreated.Hook(func(conflictID iotago.TransactionID) {
		fmt.Printf("%s > [%s] ConflictDAG.ConflictCreated: %s\n", n.Name, engineName, conflictID)
	})

	events.ConflictDAG.ConflictEvicted.Hook(func(conflictID iotago.TransactionID) {
		fmt.Printf("%s > [%s] ConflictDAG.ConflictEvicted: %s\n", n.Name, engineName, conflictID)
	})
	events.ConflictDAG.ConflictRejected.Hook(func(conflictID iotago.TransactionID) {
		fmt.Printf("%s > [%s] ConflictDAG.ConflictRejected: %s\n", n.Name, engineName, conflictID)
	})

	events.ConflictDAG.ConflictAccepted.Hook(func(conflictID iotago.TransactionID) {
		fmt.Printf("%s > [%s] ConflictDAG.ConflictAccepted: %s\n", n.Name, engineName, conflictID)
	})

	instance.Ledger.OnTransactionAttached(func(transactionMetadata mempool.TransactionMetadata) {
		fmt.Printf("%s > [%s] Ledger.TransactionAttached: %s\n", n.Name, engineName, transactionMetadata.ID())

		transactionMetadata.OnSolid(func() {
			fmt.Printf("%s > [%s] MemPool.TransactionSolid: %s\n", n.Name, engineName, transactionMetadata.ID())
		})

		transactionMetadata.OnExecuted(func() {
			fmt.Printf("%s > [%s] MemPool.TransactionExecuted: %s\n", n.Name, engineName, transactionMetadata.ID())
		})

		transactionMetadata.OnBooked(func() {
			fmt.Printf("%s > [%s] MemPool.TransactionBooked: %s\n", n.Name, engineName, transactionMetadata.ID())
		})

		transactionMetadata.OnConflicting(func() {
			fmt.Printf("%s > [%s] MemPool.TransactionConflicting: %s\n", n.Name, engineName, transactionMetadata.ID())
		})

		transactionMetadata.OnAccepted(func() {
			fmt.Printf("%s > [%s] MemPool.TransactionAccepted: %s\n", n.Name, engineName, transactionMetadata.ID())
		})

		transactionMetadata.OnRejected(func() {
			fmt.Printf("%s > [%s] MemPool.TransactionRejected: %s\n", n.Name, engineName, transactionMetadata.ID())
		})

		transactionMetadata.OnInvalid(func(err error) {
			fmt.Printf("%s > [%s] MemPool.TransactionInvalid(%s): %s\n", n.Name, engineName, err, transactionMetadata.ID())
		})

		transactionMetadata.OnOrphanedSlotUpdated(func(slot iotago.SlotIndex) {
			fmt.Printf("%s > [%s] MemPool.TransactiOnOrphanedSlotUpdated in slot %d: %s\n", n.Name, engineName, slot, transactionMetadata.ID())
		})

		transactionMetadata.OnCommittedSlotUpdated(func(slot iotago.SlotIndex) {
			fmt.Printf("%s > [%s] MemPool.TransactiOnCommittedSlotUpdated in slot %d: %s\n", n.Name, engineName, slot, transactionMetadata.ID())
		})

		transactionMetadata.OnPending(func() {
			fmt.Printf("%s > [%s] MemPool.TransactionPending: %s\n", n.Name, engineName, transactionMetadata.ID())
		})
	})
}

func (n *Node) attachEngineLogs(failOnBlockFiltered bool, instance *engine.Engine) {
	engineName := fmt.Sprintf("%s - %s", lo.Cond(n.Protocol.MainEngineInstance() != instance, "Candidate", "Main"), instance.Name()[:8])

	n.attachEngineLogsWithName(failOnBlockFiltered, instance, engineName)
}

func (n *Node) Wait() {
	n.Workers.WaitChildren()
}

func (n *Node) Shutdown() {
	stopped := make(chan struct{}, 1)

	if n.Protocol != nil {
		n.Protocol.HookStopped(func() {
			close(stopped)
		})
	} else {
		close(stopped)
	}

	if n.ctxCancel != nil {
		n.ctxCancel()
	}

	<-stopped
}

func (n *Node) ProtocolParametersHash() iotago.Identifier {
	if n.protocolParametersHash == iotago.EmptyIdentifier {
		return lo.PanicOnErr(n.Protocol.LatestAPI().ProtocolParameters().Hash())
	}

	return n.protocolParametersHash
}

func (n *Node) SetProtocolParametersHash(hash iotago.Identifier) {
	n.protocolParametersHash = hash
}

func (n *Node) HighestSupportedVersion() iotago.Version {
	if n.highestSupportedVersion == 0 {
		return n.Protocol.LatestAPI().Version()
	}

	return n.highestSupportedVersion
}

func (n *Node) SetHighestSupportedVersion(version iotago.Version) {
	n.highestSupportedVersion = version
}

func (n *Node) ForkDetectedCount() int {
	return int(n.forkDetectedCount.Load())
}

func (n *Node) CandidateEngineActivatedCount() int {
	return int(n.candidateEngineActivatedCount.Load())
}

func (n *Node) MainEngineSwitchedCount() int {
	return int(n.mainEngineSwitchedCount.Load())
}

func (n *Node) AttachedBlocks() []*blocks.Block {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	return n.attachedBlocks
}
