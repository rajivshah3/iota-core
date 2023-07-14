package retainer

import (
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/retainer"
	"github.com/iotaledger/iota-core/pkg/storage/prunable"
	iotago "github.com/iotaledger/iota.go/v4"
)

// Retainer keeps and resolves all the information needed in the API and INX.
type Retainer struct {
	protocol     *protocol.Protocol
	retainerFunc func(iotago.SlotIndex) *prunable.Retainer

	module.Module
}

// the retainer should store the "confirmed" flag of blocks between they got committed and finalized.
// this storage should have buckets and the confirmed info should be stored by simply setting the blockid.

// several intervals to prune => triggered by the pruning manager
//
//	=> the confirmed flag until it got finalized (is this always the same interval?)
//	=> the info about conflicting blocks (maybe 1 - 2 epochs)
//
// maybe also store the orphaned block there as well?

// TODO write provider
func New(retainerFunc func(iotago.SlotIndex) *prunable.Retainer) *Retainer {
	return &Retainer{
		retainerFunc: retainerFunc,
	}
}

// NewProvider creates a new SyncManager provider.
func NewProvider() module.Provider[*engine.Engine, retainer.Retainer] {
	return module.Provide(func(e *engine.Engine) retainer.Retainer {
		r := New(e.Storage.Retainer)
		asyncOpt := event.WithWorkerPool(e.Workers.CreatePool("Retainer", 1))

		e.Events.BlockDAG.BlockAttached.Hook(func(b *blocks.Block) {
			r.onBlockAttached(b.ID())
		}, asyncOpt)

		e.Events.BlockGadget.BlockAccepted.Hook(func(b *blocks.Block) {
			r.onBlockAccepted(b.ID())
		}, asyncOpt)

		e.Events.BlockGadget.BlockConfirmed.Hook(func(b *blocks.Block) {
			r.onBlockConfirmed(b.ID())
		}, asyncOpt)

		r.TriggerInitialized()

		return r
	})
}

func (r *Retainer) Shutdown() {
	r.retainerFunc = nil
}

func (r *Retainer) Block(blockID iotago.BlockID) (*model.Block, error) {
	block, _ := r.protocol.MainEngineInstance().Block(blockID)
	if block == nil {
		return nil, ierrors.Errorf("block not found: %s", blockID.ToHex())
	}

	return block, nil
}

func (r *Retainer) BlockMetadata(blockID iotago.BlockID) (*model.Block, error) {
	return nil, nil
}

func (r *Retainer) onBlockAttached(blockID iotago.BlockID) {
	retainerStore := r.retainerFunc(blockID.Index())
	err := retainerStore.Store(blockID)
	if err != nil {

	}
}

func (r *Retainer) onBlockAccepted(blockID iotago.BlockID) {
	retainerStore := r.retainerFunc(blockID.Index())
	err := retainerStore.StoreAccepted(blockID)
	if err != nil {

	}
}

func (r *Retainer) onBlockConfirmed(blockID iotago.BlockID) {
	retainerStore := r.retainerFunc(blockID.Index())
	err := retainerStore.StoreConfirmed(blockID)
	if err != nil {

	}
}
