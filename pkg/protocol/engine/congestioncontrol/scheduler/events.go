package scheduler

import (
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
)

type Events struct {
	// BlockEnqueued is triggered when a block is added to the queue.
	BlockEnqueued *event.Event1[*blocks.Block]
	// BlockScheduled is triggered when a block is scheduled.
	BlockScheduled *event.Event1[*blocks.Block]
	// BlockSkipped is triggered when a block in the buffer is accepted.
	// Skipping a block has the same effect as scheduling it, i.e., it is passed to tip manager and gossiped.
	BlockSkipped *event.Event1[*blocks.Block]
	// BlockDropped is triggered when a block in the buffer is dropped. Dropped blocks are not passed to tip manager and not gossiped.
	BlockDropped *event.Event2[*blocks.Block, error]

	event.Group[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.CreateGroupConstructor(func() (newEvents *Events) {
	return &Events{
		BlockEnqueued:  event.New1[*blocks.Block](),
		BlockScheduled: event.New1[*blocks.Block](),
		BlockSkipped:   event.New1[*blocks.Block](),
		BlockDropped:   event.New2[*blocks.Block, error](),
	}
})