package chainmanagerv1

import (
	"github.com/iotaledger/hive.go/ds/reactive"
	iotago "github.com/iotaledger/iota.go/v4"
)

// chainThresholds is a reactive component that provides a set of thresholds that are derived from the chain.
type chainThresholds struct {
	// latestIndex is the index of the latest Commitment object in the chain.
	latestIndex reactive.Variable[iotago.SlotIndex]

	// latestAttestedIndex is the index of the latest attested Commitment object in the chain.
	latestAttestedIndex reactive.Variable[iotago.SlotIndex]

	// latestVerifiedIndex is the index of the latest verified Commitment object in the chain.
	latestVerifiedIndex reactive.Variable[iotago.SlotIndex]

	// syncThreshold is the upper bound for slots that are being fed to the engine (to prevent memory exhaustion).
	syncThreshold reactive.Variable[iotago.SlotIndex]

	// warpSyncThreshold defines an offset from latest index where the warp sync process starts (we don't request slots
	// that we are about to commit ourselves).
	warpSyncThreshold reactive.Variable[iotago.SlotIndex]
}

// newChainThresholds creates a new chainThresholds instance.
func newChainThresholds(chain *Chain) *chainThresholds {
	c := &chainThresholds{
		latestIndex:         reactive.NewDerivedVariable[iotago.SlotIndex](zeroValueIfNil((*Commitment).Index), chain.latestCommitment),
		latestAttestedIndex: reactive.NewDerivedVariable[iotago.SlotIndex](zeroValueIfNil((*Commitment).Index), chain.latestAttestedCommitment),
		latestVerifiedIndex: reactive.NewDerivedVariable[iotago.SlotIndex](zeroValueIfNil((*Commitment).Index), chain.latestVerifiedCommitment),
	}

	c.warpSyncThreshold = reactive.NewDerivedVariable[iotago.SlotIndex](func(latestIndex iotago.SlotIndex) iotago.SlotIndex {
		return latestIndex - WarpSyncOffset
	}, c.latestIndex)

	c.syncThreshold = reactive.NewDerivedVariable[iotago.SlotIndex](func(latestVerifiedIndex iotago.SlotIndex) iotago.SlotIndex {
		return latestVerifiedIndex + 1 + SyncWindow
	}, c.latestVerifiedIndex)

	return c
}

// LatestIndex returns a reactive variable that always contains the index of the latest Commitment
// object in the chain.
func (c *chainThresholds) LatestIndex() reactive.Variable[iotago.SlotIndex] {
	return c.latestIndex
}

// LatestAttestedIndex returns a reactive variable that contains the index of the latest attested
// Commitment object in the chain.
func (c *chainThresholds) LatestAttestedIndex() reactive.Variable[iotago.SlotIndex] {
	return c.latestAttestedIndex
}

// LatestVerifiedIndex returns a reactive variable that contains the index of the latest verified
// Commitment object in the chain.
func (c *chainThresholds) LatestVerifiedIndex() reactive.Variable[iotago.SlotIndex] {
	return c.latestVerifiedIndex
}

// SyncThreshold returns a reactive variable that contains the upper bound for slots that are being fed to the
// engine (to prevent memory exhaustion).
func (c *chainThresholds) SyncThreshold() reactive.Variable[iotago.SlotIndex] {
	return c.syncThreshold
}

// WarpSyncThreshold returns a reactive variable that contains an offset from latest index where the warp sync
// process starts (we don't request slots that we are about to commit ourselves).
func (c *chainThresholds) WarpSyncThreshold() reactive.Variable[iotago.SlotIndex] {
	return c.warpSyncThreshold
}