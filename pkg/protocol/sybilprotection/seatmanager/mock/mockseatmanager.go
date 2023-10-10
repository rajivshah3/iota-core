package mock

import (
	"fmt"

	"github.com/iotaledger/hive.go/ds"
	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/iota-core/pkg/core/account"
	"github.com/iotaledger/iota-core/pkg/protocol/engine"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection/seatmanager"
	"github.com/iotaledger/iota-core/pkg/storage/prunable/epochstore"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/tpkg"
)

type ManualPOA struct {
	events         *seatmanager.Events
	committeeStore *epochstore.Store[*account.Accounts]

	accounts  *account.Accounts
	committee *account.SeatedAccounts
	online    ds.Set[account.SeatIndex]
	aliases   *shrinkingmap.ShrinkingMap[string, iotago.AccountID]

	module.Module
}

func NewManualPOA(committeeStore *epochstore.Store[*account.Accounts]) *ManualPOA {
	m := &ManualPOA{
		events:         seatmanager.NewEvents(),
		committeeStore: committeeStore,
		accounts:       account.NewAccounts(),
		online:         ds.NewSet[account.SeatIndex](),
		aliases:        shrinkingmap.New[string, iotago.AccountID](),
	}
	m.committee = m.accounts.SelectCommittee()

	return m
}

func NewManualPOAProvider() module.Provider[*engine.Engine, seatmanager.SeatManager] {
	return module.Provide(func(e *engine.Engine) seatmanager.SeatManager {
		poa := NewManualPOA(e.Storage.Committee())
		e.Events.CommitmentFilter.BlockAllowed.Hook(func(block *blocks.Block) {
			poa.events.BlockProcessed.Trigger(block)
		})

		e.Events.SeatManager.LinkTo(poa.events)

		return poa
	})
}

func (m *ManualPOA) AddRandomAccount(alias string) iotago.AccountID {
	id := iotago.AccountID(tpkg.Rand32ByteArray())
	id.RegisterAlias(alias)
	m.accounts.Set(id, &account.Pool{}) // We don't care about pools with PoA
	m.aliases.Set(alias, id)
	m.committee.Set(account.SeatIndex(m.committee.SeatCount()), id)
	if err := m.committeeStore.Store(0, m.accounts); err != nil {
		panic(err)
	}

	return id
}

func (m *ManualPOA) AddAccount(id iotago.AccountID, alias string) iotago.AccountID {
	m.accounts.Set(id, &account.Pool{}) // We don't care about pools with PoA
	m.aliases.Set(alias, id)
	m.committee.Set(account.SeatIndex(m.committee.SeatCount()), id)
	if err := m.committeeStore.Store(0, m.accounts); err != nil {
		panic(err)
	}

	return id
}

func (m *ManualPOA) AccountID(alias string) iotago.AccountID {
	id, exists := m.aliases.Get(alias)
	if !exists {
		panic(fmt.Sprintf("alias %s does not exist", alias))
	}

	return id
}

func (m *ManualPOA) SetOnline(aliases ...string) {
	for _, alias := range aliases {
		seat, exists := m.committee.GetSeat(m.AccountID(alias))
		if !exists {
			panic(fmt.Sprintf("alias %s does not exist", alias))
		}
		m.online.Add(seat)
	}
}

func (m *ManualPOA) SetOffline(aliases ...string) {
	for _, alias := range aliases {
		seat, exists := m.committee.GetSeat(m.AccountID(alias))
		if !exists {
			panic(fmt.Sprintf("alias %s does not exist", alias))
		}
		m.online.Delete(seat)
	}
}

func (m *ManualPOA) Accounts() *account.Accounts {
	return m.accounts
}

func (m *ManualPOA) CommitteeInSlot(_ iotago.SlotIndex) (*account.SeatedAccounts, bool) {
	return m.committee, true
}

func (m *ManualPOA) CommitteeInEpoch(_ iotago.EpochIndex) (*account.SeatedAccounts, bool) {
	return m.committee, true
}

func (m *ManualPOA) OnlineCommittee() ds.Set[account.SeatIndex] {
	return m.online
}

func (m *ManualPOA) SeatCount() int {
	return m.committee.SeatCount()
}

func (m *ManualPOA) RotateCommittee(epoch iotago.EpochIndex, _ *account.Accounts) (*account.SeatedAccounts, error) {
	if err := m.committeeStore.Store(epoch, m.accounts); err != nil {
		panic(err)
	}

	return m.committee, nil
}

func (m *ManualPOA) SetCommittee(epoch iotago.EpochIndex, validators *account.Accounts) error {
	m.accounts = validators
	m.committee = m.accounts.SelectCommittee(validators.IDs()...)
	if err := m.committeeStore.Store(epoch, m.accounts); err != nil {
		panic(err)
	}

	return nil
}

func (m *ManualPOA) InitializeCommittee(_ iotago.EpochIndex) error {
	return nil
}

func (m *ManualPOA) Shutdown() {}

var _ seatmanager.SeatManager = &ManualPOA{}
