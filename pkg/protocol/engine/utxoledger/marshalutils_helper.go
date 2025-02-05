package utxoledger

import (
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/serializer/v2/marshalutil"
	iotago "github.com/iotaledger/iota.go/v4"
)

func ParseOutputID(ms *marshalutil.MarshalUtil) (iotago.OutputID, error) {
	bytes, err := ms.ReadBytes(iotago.OutputIDLength)
	if err != nil {
		return iotago.EmptyOutputID, err
	}

	return iotago.OutputID(bytes), nil
}

func parseTransactionID(ms *marshalutil.MarshalUtil) (iotago.TransactionID, error) {
	bytes, err := ms.ReadBytes(iotago.TransactionIDLength)
	if err != nil {
		return iotago.EmptyTransactionID, err
	}

	return iotago.TransactionID(bytes), nil
}

func ParseBlockID(ms *marshalutil.MarshalUtil) (iotago.BlockID, error) {
	bytes, err := ms.ReadBytes(iotago.BlockIDLength)
	if err != nil {
		return iotago.EmptyBlockID, err
	}

	return iotago.BlockID(bytes), nil
}

func parseSlotIndex(ms *marshalutil.MarshalUtil) (iotago.SlotIndex, error) {
	bytes, err := ms.ReadBytes(iotago.SlotIndexLength)
	if err != nil {
		return 0, err
	}

	return lo.DropCount(iotago.SlotIndexFromBytes(bytes))
}
