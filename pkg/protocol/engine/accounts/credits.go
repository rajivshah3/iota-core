package accounts

import (
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/serializer/v2/marshalutil"
	iotago "github.com/iotaledger/iota.go/v4"
)

// BlockIssuanceCredits is a weight annotated with the slot it was last updated in.
type BlockIssuanceCredits struct {
	Value      iotago.BlockIssuanceCredits
	UpdateTime iotago.SlotIndex
}

// NewBlockIssuanceCredits creates a new Credits instance.
func NewBlockIssuanceCredits(value iotago.BlockIssuanceCredits, updateTime iotago.SlotIndex) (newCredits *BlockIssuanceCredits) {
	return &BlockIssuanceCredits{
		Value:      value,
		UpdateTime: updateTime,
	}
}

// Bytes returns a serialized version of the Credits.
func (c BlockIssuanceCredits) Bytes() ([]byte, error) {
	m := marshalutil.New()

	m.WriteInt64(int64(c.Value))
	m.WriteUint32(uint32(c.UpdateTime))

	return m.Bytes(), nil
}

// FromBytes parses a serialized version of the Credits.
func (c *BlockIssuanceCredits) FromBytes(bytes []byte) (int, error) {
	m := marshalutil.New(bytes)

	c.Value = iotago.BlockIssuanceCredits(lo.PanicOnErr(m.ReadInt64()))
	c.UpdateTime = iotago.SlotIndex(lo.PanicOnErr(m.ReadUint32()))

	return m.ReadOffset(), nil
}

// Update updates the Credits increasing Value and updateTime.
func (c *BlockIssuanceCredits) Update(change iotago.BlockIssuanceCredits, updateTime ...iotago.SlotIndex) {
	c.Value += change
	if len(updateTime) > 0 {
		c.UpdateTime = updateTime[0]
	}
}
