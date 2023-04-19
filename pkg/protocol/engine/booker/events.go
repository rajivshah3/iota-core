package booker

import (
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
)

type Events struct {
	BlockBooked  *event.Event1[*blocks.Block]
	WitnessAdded *event.Event1[*blocks.Block]
	Error        *event.Event1[error]

	event.Group[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.CreateGroupConstructor(func() (newEvents *Events) {
	return &Events{
		BlockBooked:  event.New1[*blocks.Block](),
		WitnessAdded: event.New1[*blocks.Block](),
		Error:        event.New1[error](),
	}
})