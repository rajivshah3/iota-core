package commitmentfilter

import (
	"github.com/iotaledger/hive.go/runtime/module"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
)

type CommitmentFilter interface {
	// ProcessPreFilteredBlock processes block from the given source.
	ProcessPreFilteredBlock(block *blocks.Block)

	module.Interface
}
