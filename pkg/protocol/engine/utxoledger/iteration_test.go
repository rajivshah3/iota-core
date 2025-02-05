//nolint:forcetypeassert,varnamelen,revive,exhaustruct // we don't care about these linters in test cases
package utxoledger_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hive.go/kvstore/mapdb"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/utxoledger"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/utxoledger/tpkg"
	"github.com/iotaledger/iota-core/pkg/utils"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
	iotago_tpkg "github.com/iotaledger/iota.go/v4/tpkg"
)

func TestUTXOComputeBalance(t *testing.T) {
	manager := utxoledger.New(mapdb.NewMapDB(), api.SingleVersionProvider(iotago_tpkg.TestAPI))

	initialOutput := tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputBasic, utils.RandAddress(iotago.AddressEd25519), 2_134_656_365)
	require.NoError(t, manager.AddGenesisUnspentOutput(initialOutput))
	require.NoError(t, manager.AddGenesisUnspentOutput(tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputAccount, utils.RandAddress(iotago.AddressAccount), 56_549_524)))
	require.NoError(t, manager.AddGenesisUnspentOutput(tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputFoundry, utils.RandAddress(iotago.AddressAccount), 25_548_858)))
	require.NoError(t, manager.AddGenesisUnspentOutput(tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputNFT, utils.RandAddress(iotago.AddressEd25519), 545_699_656)))
	require.NoError(t, manager.AddGenesisUnspentOutput(tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputBasic, utils.RandAddress(iotago.AddressAccount), 626_659_696)))

	index := iotago.SlotIndex(756)

	outputs := utxoledger.Outputs{
		tpkg.RandLedgerStateOutputOnAddressWithAmount(iotago.OutputBasic, utils.RandAddress(iotago.AddressNFT), 2_134_656_365),
	}

	spents := utxoledger.Spents{
		tpkg.RandLedgerStateSpentWithOutput(initialOutput, index),
	}

	require.NoError(t, manager.ApplyDiffWithoutLocking(index, outputs, spents))

	spent, err := manager.SpentOutputs()
	require.NoError(t, err)
	require.Equal(t, 1, len(spent))

	unspent, err := manager.UnspentOutputs()
	require.NoError(t, err)
	require.Equal(t, 5, len(unspent))

	balance, count, err := manager.ComputeLedgerBalance()
	require.NoError(t, err)
	require.Equal(t, 5, count)
	require.Equal(t, iotago.BaseToken(2_134_656_365+56_549_524+25_548_858+545_699_656+626_659_696), balance)
}

func TestUTXOIteration(t *testing.T) {
	manager := utxoledger.New(mapdb.NewMapDB(), api.SingleVersionProvider(iotago_tpkg.TestAPI))

	outputs := utxoledger.Outputs{
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressNFT)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressNFT)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputBasic, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputNFT, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputNFT, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputNFT, utils.RandAddress(iotago.AddressNFT)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputNFT, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputAccount, utils.RandAddress(iotago.AddressEd25519)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputFoundry, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputFoundry, utils.RandAddress(iotago.AddressAccount)),
		tpkg.RandLedgerStateOutputOnAddress(iotago.OutputFoundry, utils.RandAddress(iotago.AddressAccount)),
	}

	index := iotago.SlotIndex(756)

	spents := utxoledger.Spents{
		tpkg.RandLedgerStateSpentWithOutput(outputs[3], index),
		tpkg.RandLedgerStateSpentWithOutput(outputs[2], index),
		tpkg.RandLedgerStateSpentWithOutput(outputs[9], index),
	}

	require.NoError(t, manager.ApplyDiffWithoutLocking(index, outputs, spents))

	// Prepare values to check
	outputByID := make(map[string]struct{})
	unspentByID := make(map[string]struct{})
	spentByID := make(map[string]struct{})

	for _, output := range outputs {
		outputByID[output.MapKey()] = struct{}{}
		unspentByID[output.MapKey()] = struct{}{}
	}
	for _, spent := range spents {
		spentByID[spent.MapKey()] = struct{}{}
		delete(unspentByID, spent.MapKey())
	}

	// Test iteration without filters
	require.NoError(t, manager.ForEachOutput(func(output *utxoledger.Output) bool {
		_, has := outputByID[output.MapKey()]
		require.True(t, has)
		delete(outputByID, output.MapKey())

		return true
	}))

	require.Empty(t, outputByID)

	require.NoError(t, manager.ForEachUnspentOutput(func(output *utxoledger.Output) bool {
		_, has := unspentByID[output.MapKey()]
		require.True(t, has)
		delete(unspentByID, output.MapKey())

		return true
	}))
	require.Empty(t, unspentByID)

	require.NoError(t, manager.ForEachSpentOutput(func(spent *utxoledger.Spent) bool {
		_, has := spentByID[spent.MapKey()]
		require.True(t, has)
		delete(spentByID, spent.MapKey())

		return true
	}))

	require.Empty(t, spentByID)
}
