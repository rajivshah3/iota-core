package slotstore

import (
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/serializer/v2/marshalutil"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/nodeclient/apimodels"
)

const (
	blockStorePrefix byte = iota
	transactionStorePrefix
)

type BlockRetainerData struct {
	State         apimodels.BlockState
	FailureReason apimodels.BlockFailureReason
}

func (b *BlockRetainerData) Bytes() ([]byte, error) {
	marshalUtil := marshalutil.New(2)
	marshalUtil.WriteUint8(uint8(b.State))
	marshalUtil.WriteUint8(uint8(b.FailureReason))

	return marshalUtil.Bytes(), nil
}

func (b *BlockRetainerData) FromBytes(bytes []byte) (int, error) {
	marshalUtil := marshalutil.New(bytes)
	state, err := marshalUtil.ReadUint8()
	if err != nil {
		return 0, err
	}
	b.State = apimodels.BlockState(state)

	reason, err := marshalUtil.ReadUint8()
	if err != nil {
		return 0, err
	}
	b.FailureReason = apimodels.BlockFailureReason(reason)

	return marshalUtil.ReadOffset(), nil
}

type TransactionRetainerData struct {
	State         apimodels.TransactionState
	FailureReason apimodels.TransactionFailureReason
}

func (t *TransactionRetainerData) Bytes() ([]byte, error) {
	marshalUtil := marshalutil.New(2)
	marshalUtil.WriteUint8(uint8(t.State))
	marshalUtil.WriteUint8(uint8(t.FailureReason))

	return marshalUtil.Bytes(), nil
}

func (t *TransactionRetainerData) FromBytes(bytes []byte) (int, error) {
	marshalUtil := marshalutil.New(bytes)
	state, err := marshalUtil.ReadUint8()
	if err != nil {
		return 0, err
	}
	t.State = apimodels.TransactionState(state)

	reason, err := marshalUtil.ReadUint8()
	if err != nil {
		return 0, err
	}
	t.FailureReason = apimodels.TransactionFailureReason(reason)

	return marshalUtil.ReadOffset(), nil
}

type Retainer struct {
	slot       iotago.SlotIndex
	blockStore *kvstore.TypedStore[iotago.BlockID, *BlockRetainerData]
	// we store transaction metadata per blockID as in API requests we always request by blockID
	transactionStore *kvstore.TypedStore[iotago.BlockID, *TransactionRetainerData]
}

func NewRetainer(slot iotago.SlotIndex, store kvstore.KVStore) (newRetainer *Retainer) {
	return &Retainer{
		slot: slot,
		blockStore: kvstore.NewTypedStore(lo.PanicOnErr(store.WithExtendedRealm(kvstore.Realm{blockStorePrefix})),
			iotago.BlockID.Bytes,
			iotago.BlockIDFromBytes,
			(*BlockRetainerData).Bytes,
			func(bytes []byte) (*BlockRetainerData, int, error) {
				b := new(BlockRetainerData)
				c, err := b.FromBytes(bytes)

				return b, c, err
			},
		),
		transactionStore: kvstore.NewTypedStore(lo.PanicOnErr(store.WithExtendedRealm(kvstore.Realm{transactionStorePrefix})),
			iotago.BlockID.Bytes,
			iotago.BlockIDFromBytes,
			(*TransactionRetainerData).Bytes,
			func(bytes []byte) (*TransactionRetainerData, int, error) {
				t := new(TransactionRetainerData)
				c, err := t.FromBytes(bytes)

				return t, c, err
			},
		),
	}
}

func (r *Retainer) StoreBlockAttached(blockID iotago.BlockID) error {
	return r.blockStore.Set(blockID, &BlockRetainerData{
		State:         apimodels.BlockStatePending,
		FailureReason: apimodels.BlockFailureNone,
	})
}

func (r *Retainer) GetBlock(blockID iotago.BlockID) (*BlockRetainerData, bool) {
	blockData, err := r.blockStore.Get(blockID)
	if err != nil {
		return nil, false
	}

	return blockData, true
}

func (r *Retainer) GetTransaction(blockID iotago.BlockID) (*TransactionRetainerData, bool) {
	txData, err := r.transactionStore.Get(blockID)
	if err != nil {
		return nil, false
	}

	return txData, true
}

func (r *Retainer) StoreBlockAccepted(blockID iotago.BlockID) error {
	return r.blockStore.Set(blockID, &BlockRetainerData{
		State:         apimodels.BlockStateAccepted,
		FailureReason: apimodels.BlockFailureNone,
	})
}

func (r *Retainer) StoreBlockConfirmed(blockID iotago.BlockID) error {
	return r.blockStore.Set(blockID, &BlockRetainerData{
		State:         apimodels.BlockStateConfirmed,
		FailureReason: apimodels.BlockFailureNone,
	})
}

func (r *Retainer) StoreTransactionPending(blockID iotago.BlockID) error {
	return r.transactionStore.Set(blockID, &TransactionRetainerData{
		State:         apimodels.TransactionStatePending,
		FailureReason: apimodels.TxFailureNone,
	})
}

func (r *Retainer) StoreTransactionNoFailureStatus(blockID iotago.BlockID, status apimodels.TransactionState) error {
	if status == apimodels.TransactionStateFailed {
		return ierrors.Errorf("failed to retain transaction status, status cannot be failed, blockID: %s", blockID.String())
	}

	return r.transactionStore.Set(blockID, &TransactionRetainerData{
		State:         status,
		FailureReason: apimodels.TxFailureNone,
	})
}

func (r *Retainer) DeleteTransactionData(prevID iotago.BlockID) error {
	return r.transactionStore.Delete(prevID)
}

func (r *Retainer) StoreBlockFailure(blockID iotago.BlockID, failureType apimodels.BlockFailureReason) error {
	return r.blockStore.Set(blockID, &BlockRetainerData{
		State:         apimodels.BlockStateFailed,
		FailureReason: failureType,
	})
}

func (r *Retainer) StoreTransactionFailure(blockID iotago.BlockID, failureType apimodels.TransactionFailureReason) error {
	return r.transactionStore.Set(blockID, &TransactionRetainerData{
		State:         apimodels.TransactionStateFailed,
		FailureReason: failureType,
	})
}
