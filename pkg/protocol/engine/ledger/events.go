package ledger

import (
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/utxoledger"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Events struct {
	StateDiffApplied *event.Event3[iotago.SlotIndex, utxoledger.Outputs, utxoledger.Spents]
	AccountCreated   *event.Event1[iotago.AccountID]
	AccountDestroyed *event.Event1[iotago.AccountID]

	event.Group[Events, *Events]
}

// NewEvents contains the constructor of the Events object (it is generated by a generic factory).
var NewEvents = event.CreateGroupConstructor(func() (newEvents *Events) {
	return &Events{
		StateDiffApplied: event.New3[iotago.SlotIndex, utxoledger.Outputs, utxoledger.Spents](),
		AccountCreated:   event.New1[iotago.AccountID](),
		AccountDestroyed: event.New1[iotago.AccountID](),
	}
})
