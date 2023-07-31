package testsuite

import (
	"context"
	"fmt"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/blockfactory"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (t *TestSuite) assertParentsExistFromBlockOptions(blockOpts []options.Option[blockfactory.BlockParams], node *mock.Node) {
	params := options.Apply(&blockfactory.BlockParams{}, blockOpts)
	parents := params.References[iotago.StrongParentType]
	parents = append(parents, params.References[iotago.WeakParentType]...)
	parents = append(parents, params.References[iotago.ShallowLikeParentType]...)

	t.AssertBlocksExist(t.Blocks(lo.Map(parents, func(id iotago.BlockID) string { return id.Alias() })...), true, node)
}

func (t *TestSuite) RegisterBlock(alias string, block *blocks.Block) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.registerBlock(alias, block)
}

func (t *TestSuite) registerBlock(alias string, block *blocks.Block) {
	t.blocks.Set(alias, block)
	block.ID().RegisterAlias(alias)
}

func (t *TestSuite) CreateBlock(alias string, node *mock.Node, blockOpts ...options.Option[blockfactory.BlockParams]) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	block := node.CreateBlock(context.Background(), alias, blockOpts...)

	t.registerBlock(alias, block)
}

func (t *TestSuite) IssueBlockAtSlot(alias string, slot iotago.SlotIndex, slotCommitment *iotago.Commitment, node *mock.Node, parents ...iotago.BlockID) *blocks.Block {
	t.AssertBlocksExist(t.Blocks(lo.Map(parents, func(id iotago.BlockID) string { return id.Alias() })...), true, node)

	t.mutex.Lock()
	defer t.mutex.Unlock()

	timeProvider := t.API.TimeProvider()
	issuingTime := timeProvider.SlotStartTime(slot).Add(time.Duration(t.uniqueCounter.Add(1)))

	require.Truef(t.Testing, issuingTime.Before(time.Now()), "node: %s: issued block (%s, slot: %d) is in the current (%s, slot: %d) or future slot", node.Name, issuingTime, slot, time.Now(), timeProvider.SlotFromTime(time.Now()))

	block := node.IssueBlock(context.Background(), alias, blockfactory.WithIssuingTime(issuingTime), blockfactory.WithSlotCommitment(slotCommitment), blockfactory.WithStrongParents(parents...))

	t.registerBlock(alias, block)

	return block
}

func (t *TestSuite) IssueValidationBlockAtSlotWithOptions(alias string, slot iotago.SlotIndex, node *mock.Node, blockOpts ...options.Option[blockfactory.BlockParams]) *blocks.Block {
	t.assertParentsExistFromBlockOptions(blockOpts, node)

	t.mutex.Lock()
	defer t.mutex.Unlock()

	timeProvider := t.API.TimeProvider()

	issuingTime := timeProvider.SlotStartTime(slot).Add(time.Duration(t.uniqueCounter.Add(1)))

	require.Truef(t.Testing, issuingTime.Before(time.Now()), "node: %s: issued block (%s, slot: %d) is in the current (%s, slot: %d) or future slot", node.Name, issuingTime, slot, time.Now(), timeProvider.SlotFromTime(time.Now()))

	block := node.IssueValidationBlock(context.Background(), alias, append(blockOpts, blockfactory.WithIssuingTime(issuingTime))...)

	t.registerBlock(alias, block)

	return block
}

func (t *TestSuite) IssueBlockAtSlotWithOptions(alias string, slot iotago.SlotIndex, slotCommitment *iotago.Commitment, node *mock.Node, blockOpts ...options.Option[blockfactory.BlockParams]) *blocks.Block {
	t.assertParentsExistFromBlockOptions(blockOpts, node)

	t.mutex.Lock()
	defer t.mutex.Unlock()

	timeProvider := t.API.TimeProvider()
	issuingTime := timeProvider.SlotStartTime(slot).Add(time.Duration(t.uniqueCounter.Add(1)))

	require.Truef(t.Testing, issuingTime.Before(time.Now()), "node: %s: issued block (%s, slot: %d) is in the current (%s, slot: %d) or future slot", node.Name, issuingTime, slot, time.Now(), timeProvider.SlotFromTime(time.Now()))

	block := node.IssueBlock(context.Background(), alias, append(blockOpts, blockfactory.WithIssuingTime(issuingTime), blockfactory.WithSlotCommitment(slotCommitment))...)

	t.registerBlock(alias, block)

	return block
}

func (t *TestSuite) IssueBlock(alias string, node *mock.Node, blockOpts ...options.Option[blockfactory.BlockParams]) *blocks.Block {
	t.assertParentsExistFromBlockOptions(blockOpts, node)

	t.mutex.Lock()
	defer t.mutex.Unlock()

	block := node.IssueBlock(context.Background(), alias, blockOpts...)

	t.registerBlock(alias, block)

	return block
}

func (t *TestSuite) IssueBlockRowInSlot(slot iotago.SlotIndex, row int, parentsPrefixAlias string, nodes []*mock.Node, issuingOptions map[string][]options.Option[blockfactory.BlockParams]) []*blocks.Block {
	blocksIssued := make([]*blocks.Block, 0, len(nodes))

	strongParents := t.BlockIDsWithPrefix(parentsPrefixAlias)
	fmt.Println("issuing", slot, row, "strongParents", parentsPrefixAlias, strongParents)
	issuingOptionsCopy := lo.MergeMaps(make(map[string][]options.Option[blockfactory.BlockParams]), issuingOptions)

	for _, node := range nodes {
		blockAlias := fmt.Sprintf("%d.%d-%s", slot, row, node.Name)
		issuingOptionsCopy[node.Name] = append(issuingOptionsCopy[node.Name], blockfactory.WithStrongParents(strongParents...))

		b := t.IssueValidationBlockAtSlotWithOptions(blockAlias, slot, node, issuingOptionsCopy[node.Name]...)
		blocksIssued = append(blocksIssued, b)
	}

	return blocksIssued
}

func (t *TestSuite) IssueBlockRowsInSlot(slot iotago.SlotIndex, rows int, initialParentsPrefixAlias string, nodes []*mock.Node, issuingOptions map[string][]options.Option[blockfactory.BlockParams]) (allBlocksIssued []*blocks.Block, lastBlockRow []*blocks.Block) {
	var blocksIssued, lastBlockRowIssued []*blocks.Block
	parentsPrefixAlias := initialParentsPrefixAlias

	for row := 0; row < rows; row++ {
		if row > 0 {
			parentsPrefixAlias = fmt.Sprintf("%d.%d", slot, row-1)
		}

		lastBlockRowIssued = t.IssueBlockRowInSlot(slot, row, parentsPrefixAlias, nodes, issuingOptions)
		blocksIssued = append(blocksIssued, lastBlockRowIssued...)
	}

	return blocksIssued, lastBlockRowIssued
}

func (t *TestSuite) IssueBlocksAtSlots(slots []iotago.SlotIndex, rowsPerSlot int, initialParentsPrefixAlias string, nodes []*mock.Node, waitForSlotsCommitted bool, issuingOptions map[string][]options.Option[blockfactory.BlockParams]) (allBlocksIssued []*blocks.Block, lastBlockRow []*blocks.Block) {
	var blocksIssued, lastBlockRowIssued []*blocks.Block
	parentsPrefixAlias := initialParentsPrefixAlias

	for i, slot := range slots {
		if i > 0 {
			parentsPrefixAlias = fmt.Sprintf("%d.%d", slots[i-1], rowsPerSlot-1)
		}

		blocksInSlot, lastRowInSlot := t.IssueBlockRowsInSlot(slot, rowsPerSlot, parentsPrefixAlias, nodes, issuingOptions)
		blocksIssued = append(blocksIssued, blocksInSlot...)
		lastBlockRowIssued = lastRowInSlot

		if waitForSlotsCommitted {
			if slot > t.API.ProtocolParameters().MinCommittableAge()+1 {
				t.AssertCommitmentSlotIndexExists(slot-(t.API.ProtocolParameters().MinCommittableAge()+1), nodes...)
			} else {
				t.AssertBlocksExist(blocksInSlot, true, nodes...)
			}
		}
	}

	return blocksIssued, lastBlockRowIssued
}

func (t *TestSuite) IssueBlocksAtEpoch(epoch iotago.EpochIndex, rowsPerSlot int, initialParentsPrefixAlias string, nodes []*mock.Node, waitForSlotsCommitted bool, issuingOptions map[string][]options.Option[blockfactory.BlockParams]) (allBlocksIssued []*blocks.Block, lastBlockRow []*blocks.Block) {
	return t.IssueBlocksAtSlots(t.SlotsForEpoch(epoch), rowsPerSlot, initialParentsPrefixAlias, nodes, waitForSlotsCommitted, issuingOptions)
}

func (t *TestSuite) SlotsForEpoch(epoch iotago.EpochIndex) []iotago.SlotIndex {
	slotsPerEpoch := t.API.TimeProvider().EpochDurationSlots()

	slots := make([]iotago.SlotIndex, 0, slotsPerEpoch)
	epochStart := t.API.TimeProvider().EpochStart(epoch)
	for i := epochStart; i < epochStart+slotsPerEpoch; i++ {
		if i == 0 {
			continue
		}
		slots = append(slots, i)
	}

	return slots
}

func (t *TestSuite) CommitUntilSlot(slot iotago.SlotIndex, activeNodes []*mock.Node, parent *blocks.Block) *blocks.Block {

	// we need to get accepted tangle time up to slot + minCA + 1
	// first issue a chain of blocks with step size minCA up until slot + minCA + 1
	// then issue one more block to accept the last in the chain which will trigger commitment of the second last in the chain

	latestCommittedSlot := activeNodes[0].Protocol.MainEngineInstance().Storage.Settings().LatestCommitment().Index()
	if latestCommittedSlot >= slot {
		return parent
	}
	nextBlockSlot := lo.Min(slot+t.optsMinCommittableAge+1, latestCommittedSlot+t.optsMinCommittableAge+1)
	tip := parent
	chainIndex := 0
	for {
		// preacceptance of nextBlockSlot
		for _, node := range activeNodes {
			blockAlias := fmt.Sprintf("chain-%s-%d-%s", parent.ID().Alias(), chainIndex, node.Name)
			tip = t.IssueBlockAtSlot(blockAlias, nextBlockSlot, node.Protocol.MainEngineInstance().Storage.Settings().LatestCommitment().Commitment(), node, tip.ID())
		}
		// acceptance of nextBlockSlot
		for _, node := range activeNodes {
			blockAlias := fmt.Sprintf("chain-%s-%d-%s", parent.ID().Alias(), chainIndex+1, node.Name)
			tip = t.IssueBlockAtSlot(blockAlias, nextBlockSlot, node.Protocol.MainEngineInstance().Storage.Settings().LatestCommitment().Commitment(), node, tip.ID())
		}
		if nextBlockSlot == slot+t.optsMinCommittableAge+1 {
			break
		}
		nextBlockSlot = lo.Min(slot+t.optsMinCommittableAge+1, nextBlockSlot+t.optsMinCommittableAge+1)
		chainIndex += 2
	}

	for _, node := range activeNodes {
		t.AssertLatestCommitmentSlotIndex(slot, node)
	}

	return tip
}
