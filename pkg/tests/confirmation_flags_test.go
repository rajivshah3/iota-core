package tests

import (
	"testing"

	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/notarization/slotnotarization"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/sybilprotection/poa"
	"github.com/iotaledger/iota-core/pkg/testsuite"
	iotago "github.com/iotaledger/iota.go/v4"
)

func TestConfirmationFlags(t *testing.T) {
	ts := testsuite.NewTestSuite(t)
	defer ts.Shutdown()

	nodeA := ts.AddValidatorNode("nodeA", 25)
	nodeB := ts.AddValidatorNode("nodeB", 25)
	nodeC := ts.AddValidatorNode("nodeC", 25)
	nodeD := ts.AddValidatorNode("nodeD", 25)

	expectedOnlineCommittee := map[iotago.AccountID]int64{
		nodeA.AccountID: 25,
	}

	expectedCommittee := map[iotago.AccountID]int64{
		nodeA.AccountID: 25,
		nodeB.AccountID: 25,
		nodeC.AccountID: 25,
		nodeD.AccountID: 25,
	}
	ts.Run(map[string][]options.Option[protocol.Protocol]{
		"nodeA": {
			protocol.WithNotarizationProvider(
				slotnotarization.NewProvider(slotnotarization.WithMinCommittableSlotAge(1)),
			),
			protocol.WithSybilProtectionProvider(
				poa.NewProvider(expectedCommittee, poa.WithOnlineCommitteeStartup(nodeA.AccountID)),
			),
		},
		"nodeB": {
			protocol.WithNotarizationProvider(
				slotnotarization.NewProvider(slotnotarization.WithMinCommittableSlotAge(1)),
			),
			protocol.WithSybilProtectionProvider(
				poa.NewProvider(expectedCommittee, poa.WithOnlineCommitteeStartup(nodeA.AccountID)),
			),
		},
		"nodeC": {
			protocol.WithNotarizationProvider(
				slotnotarization.NewProvider(slotnotarization.WithMinCommittableSlotAge(1)),
			),
			protocol.WithSybilProtectionProvider(
				poa.NewProvider(expectedCommittee, poa.WithOnlineCommitteeStartup(nodeA.AccountID)),
			),
		},
		"nodeD": {
			protocol.WithNotarizationProvider(
				slotnotarization.NewProvider(slotnotarization.WithMinCommittableSlotAge(1)),
			),
			protocol.WithSybilProtectionProvider(
				poa.NewProvider(expectedCommittee, poa.WithOnlineCommitteeStartup(nodeA.AccountID)),
			),
		},
	})
	ts.HookLogging()

	// Verify that nodes have the expected states.
	ts.AssertNodeState(ts.Nodes(),
		testsuite.WithSnapshotImported(true),
		testsuite.WithProtocolParameters(ts.ProtocolParameters),
		testsuite.WithLatestCommitment(iotago.NewEmptyCommitment()),
		testsuite.WithLatestStateMutationSlot(0),
		testsuite.WithLatestFinalizedSlot(0),
		testsuite.WithChainID(iotago.NewEmptyCommitment().MustID()),
		testsuite.WithStorageCommitments([]*iotago.Commitment{iotago.NewEmptyCommitment()}),
		testsuite.WithSybilProtectionCommittee(expectedCommittee),
		testsuite.WithSybilProtectionOnlineCommittee(expectedOnlineCommittee),
		testsuite.WithEvictedSlot(0),
		testsuite.WithActiveRootBlocks(ts.Blocks("Genesis")),
		testsuite.WithStorageRootBlocks(ts.Blocks("Genesis")),
	)

	// Slots 1-3: only node A is online and issues blocks.
	{
		ts.IssueBlockAtSlot("A.1.0", 1, iotago.NewEmptyCommitment(), nodeA, ts.BlockID("Genesis"))
		ts.IssueBlockAtSlot("A.1.1", 1, iotago.NewEmptyCommitment(), nodeA, ts.BlockID("A.1.0"))
		ts.IssueBlockAtSlot("A.2.0", 2, iotago.NewEmptyCommitment(), nodeA, ts.BlockID("A.1.1"))
		ts.IssueBlockAtSlot("A.2.1", 2, iotago.NewEmptyCommitment(), nodeA, ts.BlockID("A.2.0"))
		ts.IssueBlockAtSlot("A.3.0", 3, iotago.NewEmptyCommitment(), nodeA, ts.BlockID("A.2.1"))

		ts.AssertBlocksInCacheAccepted(ts.Blocks("A.1.0", "A.1.1", "A.2.0", "A.2.1", "A.3.0"), true, ts.Nodes()...)
		ts.AssertBlocksInCacheRatifiedAccepted(ts.Blocks("A.1.0", "A.1.1", "A.2.0", "A.2.1"), true, ts.Nodes()...)

		ts.AssertBlocksInCacheConfirmed(ts.Blocks("A.1.0", "A.1.1", "A.2.0", "A.2.1", "A.3.0"), false, ts.Nodes()...)
		ts.AssertBlocksInCacheRatifiedConfirmed(ts.Blocks("A.1.0", "A.1.1", "A.2.0", "A.2.1", "A.3.0"), false, ts.Nodes()...)
	}

	{
		ts.IssueBlockAtSlot("A.4.0", 4, iotago.NewEmptyCommitment(), nodeA, ts.BlockID("A.3.0"))
		ts.IssueBlockAtSlot("B.4.0", 4, iotago.NewEmptyCommitment(), nodeB, ts.BlockID("A.4.0"))
		ts.IssueBlockAtSlot("A.4.1", 4, iotago.NewEmptyCommitment(), nodeA, ts.BlockID("B.4.0"))

		ts.AssertBlocksInCacheAccepted(ts.Blocks("A.4.0", "B.4.0"), true, ts.Nodes()...)
		ts.AssertBlocksInCacheRatifiedAccepted(ts.Blocks("A.3.0", "A.4.0"), true, ts.Nodes()...)

		ts.AssertNodeState(ts.Nodes(),
			testsuite.WithLatestFinalizedSlot(0),
			testsuite.WithLatestCommitmentSlotIndex(2),
			testsuite.WithSybilProtectionCommittee(expectedCommittee),
			testsuite.WithSybilProtectionOnlineCommittee(lo.MergeMaps(expectedOnlineCommittee, map[iotago.AccountID]int64{
				nodeB.AccountID: 25,
			})),
			testsuite.WithEvictedSlot(2),
		)
	}

	{
		slot1Commitment := lo.PanicOnErr(nodeA.Protocol.MainEngineInstance().Storage.Commitments().Load(1)).Commitment()
		slot2Commitment := lo.PanicOnErr(nodeA.Protocol.MainEngineInstance().Storage.Commitments().Load(2)).Commitment()

		ts.IssueBlockAtSlot("C.5.0", 5, slot1Commitment, nodeC, ts.BlockID("A.4.1"))
		ts.IssueBlockAtSlot("A.5.0", 5, slot2Commitment, nodeA, ts.BlockID("C.5.0"))
		ts.IssueBlockAtSlot("B.5.0", 5, slot2Commitment, nodeB, ts.BlockID("C.5.0"))

		ts.AssertBlocksInCacheAccepted(ts.Blocks("A.3.0", "A.4.0", "B.4.0", "C.5.0"), true, ts.Nodes()...)
		ts.AssertBlocksInCacheConfirmed(ts.Blocks("A.3.0", "A.4.0", "B.4.0", "C.5.0"), true, ts.Nodes()...)

		ts.AssertBlocksInCacheRatifiedAccepted(ts.Blocks("A.3.0", "A.4.0"), true, ts.Nodes()...)
		ts.AssertBlocksInCacheRatifiedConfirmed(ts.Blocks("A.4.0"), true, ts.Nodes()...)

		ts.AssertBlocksInCacheAccepted(ts.Blocks("A.5.0", "B.5.0"), false, ts.Nodes()...)
		ts.AssertBlocksInCacheRatifiedAccepted(ts.Blocks("B.4.0", "C.5.0"), false, ts.Nodes()...)
		ts.AssertBlocksInCacheConfirmed(ts.Blocks("A.5.0", "B.5.0"), false, ts.Nodes()...)
		ts.AssertBlocksInCacheRatifiedConfirmed(ts.Blocks("B.4.0", "A.4.1"), false, ts.Nodes()...)

		// Not ratified confirmed because slot 3 <= 5 (ratifier index) - 2 (confirmation ratification threshold).
		ts.AssertBlocksInCacheRatifiedConfirmed(ts.Blocks("A.3.0"), false, ts.Nodes()...)

		ts.AssertNodeState(ts.Nodes(),
			testsuite.WithLatestFinalizedSlot(0),
			testsuite.WithLatestCommitmentSlotIndex(2),
			testsuite.WithSybilProtectionCommittee(expectedCommittee),
			testsuite.WithSybilProtectionOnlineCommittee(lo.MergeMaps(expectedOnlineCommittee, map[iotago.AccountID]int64{
				nodeC.AccountID: 25,
			})),
			testsuite.WithEvictedSlot(2),
		)
	}

	{
		slot2Commitment := lo.PanicOnErr(nodeA.Protocol.MainEngineInstance().Storage.Commitments().Load(2)).Commitment()

		ts.IssueBlockAtSlot("A.6.0", 6, slot2Commitment, nodeA, ts.BlockIDs("A.5.0", "B.5.0")...)
		ts.IssueBlockAtSlot("B.6.0", 6, slot2Commitment, nodeB, ts.BlockIDs("A.5.0", "B.5.0")...)
		ts.IssueBlockAtSlot("C.6.0", 6, slot2Commitment, nodeC, ts.BlockIDs("A.5.0", "B.5.0")...)

		ts.IssueBlockAtSlot("A.6.1", 6, slot2Commitment, nodeA, ts.BlockIDs("A.6.0", "B.6.0", "C.6.0")...)
		ts.IssueBlockAtSlot("B.6.1", 6, slot2Commitment, nodeB, ts.BlockIDs("A.6.0", "B.6.0", "C.6.0")...)
		ts.IssueBlockAtSlot("C.6.1", 6, slot2Commitment, nodeC, ts.BlockIDs("A.6.0", "B.6.0", "C.6.0")...)

		ts.AssertBlocksInCacheAccepted(ts.Blocks("A.6.0", "B.6.0", "C.6.0"), true, ts.Nodes()...)
		ts.AssertBlocksInCacheConfirmed(ts.Blocks("A.6.0", "B.6.0", "C.6.0"), true, ts.Nodes()...)

		ts.AssertBlocksInCacheRatifiedAccepted(ts.Blocks("B.4.0", "A.4.1", "C.5.0", "A.5.0", "B.5.0"), true, ts.Nodes()...)
		ts.AssertBlocksInCacheRatifiedConfirmed(ts.Blocks("B.4.0", "A.4.1", "C.5.0", "A.5.0", "B.5.0"), true, ts.Nodes()...)

		ts.AssertBlocksInCacheAccepted(ts.Blocks("A.6.1", "B.6.1", "C.6.1"), false, ts.Nodes()...)
		ts.AssertBlocksInCacheConfirmed(ts.Blocks("A.6.1", "B.6.1", "C.6.1"), false, ts.Nodes()...)

		ts.AssertNodeState(ts.Nodes(),
			testsuite.WithLatestFinalizedSlot(1), // TODO: implement this properly
			testsuite.WithLatestCommitmentSlotIndex(3),
			testsuite.WithSybilProtectionCommittee(expectedCommittee),
			testsuite.WithSybilProtectionOnlineCommittee(expectedOnlineCommittee),
			testsuite.WithEvictedSlot(3),
		)
	}

}
