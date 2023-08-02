package testsuite

import (
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"

	"github.com/iotaledger/hive.go/crypto/ed25519"
	"github.com/iotaledger/hive.go/ds/orderedmap"
	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/iota-core/pkg/protocol"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/utxoledger"
	"github.com/iotaledger/iota-core/pkg/protocol/snapshotcreator"
	"github.com/iotaledger/iota-core/pkg/protocol/sybilprotection/sybilprotectionv1"
	"github.com/iotaledger/iota-core/pkg/storage/utils"
	"github.com/iotaledger/iota-core/pkg/testsuite/mock"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/tpkg"
)

const MinIssuerAccountDeposit = iotago.BaseToken(84400)
const MinValidatorAccountDeposit = iotago.BaseToken(88200)

type TestSuite struct {
	Testing     *testing.T
	fakeTesting *testing.T
	Network     *mock.Network

	Directory *utils.Directory
	nodes     *orderedmap.OrderedMap[string, *mock.Node]
	running   bool

	snapshotPath string
	blocks       *shrinkingmap.ShrinkingMap[string, *blocks.Block]

	API iotago.API

	optsGenesisTimestampOffset int64
	optsLivenessThreshold      iotago.SlotIndex
	optsMinCommittableAge      iotago.SlotIndex
	optsMaxCommittableAge      iotago.SlotIndex
	optsSlotsPerEpochExponent  uint8
	optsEpochNearingThreshold  iotago.SlotIndex
	optsAccounts               []snapshotcreator.AccountDetails
	optsSnapshotOptions        []options.Option[snapshotcreator.Options]
	optsWaitFor                time.Duration
	optsTick                   time.Duration

	uniqueBlockTimeCounter             atomic.Int64
	automaticTransactionIssuingCounter atomic.Int64
	mutex                              syncutils.RWMutex
	TransactionFramework               *TransactionFramework
	genesisSeed                        [32]byte
}

func NewTestSuite(testingT *testing.T, opts ...options.Option[TestSuite]) *TestSuite {
	return options.Apply(&TestSuite{
		Testing:     testingT,
		fakeTesting: &testing.T{},
		genesisSeed: tpkg.RandEd25519Seed(),
		Network:     mock.NewNetwork(),
		Directory:   utils.NewDirectory(testingT.TempDir()),
		nodes:       orderedmap.New[string, *mock.Node](),
		blocks:      shrinkingmap.New[string, *blocks.Block](),

		optsWaitFor:                DurationFromEnvOrDefault(5*time.Second, "CI_UNIT_TESTS_WAIT_FOR"),
		optsTick:                   DurationFromEnvOrDefault(2*time.Millisecond, "CI_UNIT_TESTS_TICK"),
		optsGenesisTimestampOffset: 0,
		optsLivenessThreshold:      3,
		optsMinCommittableAge:      10,
		optsMaxCommittableAge:      20,
		optsSlotsPerEpochExponent:  5,
		optsEpochNearingThreshold:  16,
	}, opts, func(t *TestSuite) {
		fmt.Println("Setup TestSuite -", testingT.Name())
		t.API = iotago.V3API(
			iotago.NewV3ProtocolParameters(
				iotago.WithNetworkOptions(
					testingT.Name(),
					"rms",
				),
				iotago.WithSupplyOptions(
					1_000_0000,
					100,
					1,
					10,
				),
				iotago.WithTimeProviderOptions(
					time.Now().Truncate(10*time.Second).Unix()-t.optsGenesisTimestampOffset,
					10,
					t.optsSlotsPerEpochExponent,
				),
				iotago.WithLivenessOptions(
					t.optsLivenessThreshold,
					t.optsMinCommittableAge,
					t.optsMaxCommittableAge,
					t.optsEpochNearingThreshold,
				),
				iotago.WithStakingOptions(1),
			),
		)

		genesisBlock := blocks.NewRootBlock(iotago.EmptyBlockID(), iotago.NewEmptyCommitment(t.API.ProtocolParameters().Version()).MustID(), time.Unix(t.API.ProtocolParameters().TimeProvider().GenesisUnixTime(), 0))
		t.RegisterBlock("Genesis", genesisBlock)

		t.snapshotPath = t.Directory.Path("genesis_snapshot.bin")
		defaultSnapshotOptions := []options.Option[snapshotcreator.Options]{
			snapshotcreator.WithDatabaseVersion(protocol.DatabaseVersion),
			snapshotcreator.WithFilePath(t.snapshotPath),
			snapshotcreator.WithProtocolParameters(t.API.ProtocolParameters()),
			snapshotcreator.WithRootBlocks(map[iotago.BlockID]iotago.CommitmentID{
				iotago.EmptyBlockID(): iotago.NewEmptyCommitment(t.API.ProtocolParameters().Version()).MustID(),
			}),
		}
		t.optsSnapshotOptions = append(defaultSnapshotOptions, t.optsSnapshotOptions...)
	})
}

// Block returns the block with the given alias. Important to note that this blocks.Block is a placeholder and is
// thus not the same as the blocks.Block that is created by a node.
func (t *TestSuite) Block(alias string) *blocks.Block {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	block, exist := t.blocks.Get(alias)
	if !exist {
		panic(fmt.Sprintf("block %s not registered", alias))
	}

	return block
}

func (t *TestSuite) AccountOutput(alias string) *utxoledger.Output {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	output, exist := t.TransactionFramework.states[alias]
	if !exist {
		panic(fmt.Sprintf("account %s not registered", alias))
	}

	if _, ok := output.Output().(*iotago.AccountOutput); !ok {
		panic(fmt.Sprintf("output %s is not an account", alias))
	}

	return output
}

func (t *TestSuite) BlockID(alias string) iotago.BlockID {
	return t.Block(alias).ID()
}

func (t *TestSuite) BlockIDs(aliases ...string) []iotago.BlockID {
	return lo.Map(aliases, func(alias string) iotago.BlockID {
		return t.BlockID(alias)
	})
}

func (t *TestSuite) Blocks(aliases ...string) []*blocks.Block {
	return lo.Map(aliases, func(alias string) *blocks.Block {
		return t.Block(alias)
	})
}

func (t *TestSuite) BlocksWithPrefix(prefix string) []*blocks.Block {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	b := make([]*blocks.Block, 0)

	t.blocks.ForEach(func(alias string, block *blocks.Block) bool {
		if strings.HasPrefix(alias, prefix) {
			b = append(b, block)
		}

		return true
	})

	return b
}

func (t *TestSuite) BlocksWithPrefixes(prefixes ...string) []*blocks.Block {
	b := make([]*blocks.Block, 0)

	for _, prefix := range prefixes {
		b = append(b, t.BlocksWithPrefix(prefix)...)
	}

	return b
}

func (t *TestSuite) BlocksWithSuffix(suffix string) []*blocks.Block {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	b := make([]*blocks.Block, 0)

	t.blocks.ForEach(func(alias string, block *blocks.Block) bool {
		if strings.HasSuffix(alias, suffix) {
			b = append(b, block)
		}

		return true
	})

	return b
}

func (t *TestSuite) BlocksWithSuffixes(suffixes ...string) []*blocks.Block {
	b := make([]*blocks.Block, 0)

	for _, prefix := range suffixes {
		b = append(b, t.BlocksWithSuffix(prefix)...)
	}

	return b
}

func (t *TestSuite) BlockIDsWithPrefix(prefix string) []iotago.BlockID {
	blocksWithPrefix := t.BlocksWithPrefix(prefix)

	return lo.Map(blocksWithPrefix, func(block *blocks.Block) iotago.BlockID {
		return block.ID()
	})
}

func (t *TestSuite) Node(name string) *mock.Node {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	node, exist := t.nodes.Get(name)
	if !exist {
		panic(fmt.Sprintf("node %s does not exist", name))
	}

	return node
}

func (t *TestSuite) Nodes(names ...string) []*mock.Node {
	if len(names) == 0 {
		t.mutex.RLock()
		defer t.mutex.RUnlock()

		nodes := make([]*mock.Node, 0, t.nodes.Size())
		t.nodes.ForEach(func(_ string, node *mock.Node) bool {
			nodes = append(nodes, node)

			return true
		})

		return nodes
	}

	nodes := make([]*mock.Node, len(names))
	for i, name := range names {
		nodes[i] = t.Node(name)
	}

	return nodes
}

func (t *TestSuite) Wait(nodes ...*mock.Node) {
	for _, node := range nodes {
		node.Wait()
	}
}

func (t *TestSuite) WaitWithDelay(delay time.Duration, nodes ...*mock.Node) {
	t.Wait(nodes...)
	time.Sleep(delay)
	t.Wait(nodes...)
}

func (t *TestSuite) Shutdown() {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	t.nodes.ForEach(func(_ string, node *mock.Node) bool {
		node.Shutdown()
		return true
	})

	// fmt.Println("======= ATTACHED BLOCKS =======")
	// t.nodes.ForEach(func(_ string, node *mock.Node) bool {
	// 	for _, block := range node.AttachedBlocks() {
	// 		fmt.Println(node.Name, ">", block)
	// 	}
	//
	// 	return true
	// })
}

func (t *TestSuite) addNodeToPartition(name string, partition string, validator bool, optDeposit ...iotago.BaseToken) *mock.Node {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if validator && t.running {
		panic(fmt.Sprintf("cannot add validator node %s to partition %s: framework already running", name, partition))
	}

	node := mock.NewNode(t.Testing, t.Network, partition, name, validator)
	t.nodes.Set(name, node)

	deposit := MinValidatorAccountDeposit
	if len(optDeposit) > 0 {
		deposit = optDeposit[0]
	}
	if deposit > 0 {
		accountDetails := snapshotcreator.AccountDetails{
			Address:    iotago.Ed25519AddressFromPubKey(node.PubKey),
			Amount:     deposit,
			Mana:       iotago.Mana(deposit),
			IssuerKey:  ed25519.PublicKey(node.PubKey),
			ExpirySlot: math.MaxUint64,
		}
		if validator {
			accountDetails.StakedAmount = accountDetails.Amount
			accountDetails.StakingEpochEnd = math.MaxUint64
			accountDetails.FixedCost = iotago.Mana(0)
		}

		t.optsAccounts = append(t.optsAccounts, accountDetails)
	}

	return node
}

func (t *TestSuite) AddValidatorNodeToPartition(name string, partition string, optDeposit ...iotago.BaseToken) *mock.Node {
	return t.addNodeToPartition(name, partition, true, optDeposit...)
}

func (t *TestSuite) AddValidatorNode(name string, optDeposit ...iotago.BaseToken) *mock.Node {
	return t.addNodeToPartition(name, mock.NetworkMainPartition, true, optDeposit...)
}

func (t *TestSuite) AddNodeToPartition(name string, partition string, optDeposit ...iotago.BaseToken) *mock.Node {
	return t.addNodeToPartition(name, partition, false, optDeposit...)
}

func (t *TestSuite) AddNode(name string, optDeposit ...iotago.BaseToken) *mock.Node {
	return t.addNodeToPartition(name, mock.NetworkMainPartition, false, optDeposit...)
}

func (t *TestSuite) RemoveNode(name string) {
	t.nodes.Delete(name)
}

func (t *TestSuite) Run(nodesOptions ...map[string][]options.Option[protocol.Protocol]) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Create accounts for any block issuer nodes added before starting the network.
	if t.optsAccounts != nil {
		wallet := mock.NewHDWallet("genesis", t.genesisSeed[:], 0)
		t.optsSnapshotOptions = append(t.optsSnapshotOptions, snapshotcreator.WithAccounts(lo.Map(t.optsAccounts, func(accountDetails snapshotcreator.AccountDetails) snapshotcreator.AccountDetails {
			// if no custom address is assigned to the account, assign an address generated from GenesisSeed
			if accountDetails.Address == nil {
				accountDetails.Address = wallet.Address()
			}

			if accountDetails.AccountID.Empty() {
				accountDetails.AccountID = blake2b.Sum256(accountDetails.IssuerKey[:])
			}

			return accountDetails
		})...))
	}

	// TODO: what if someone passes custom GenesisSeed? We set the random one anyway in the transaction framework.

	err := snapshotcreator.CreateSnapshot(append([]options.Option[snapshotcreator.Options]{snapshotcreator.WithGenesisSeed(t.genesisSeed[:])}, t.optsSnapshotOptions...)...)
	if err != nil {
		panic(fmt.Sprintf("failed to create snapshot: %s", err))
	}

	t.nodes.ForEach(func(_ string, node *mock.Node) bool {
		baseOpts := []options.Option[protocol.Protocol]{
			protocol.WithSnapshotPath(t.snapshotPath),
			protocol.WithBaseDirectory(t.Directory.PathWithCreate(node.Name)),
			protocol.WithEpochGadgetProvider(
				sybilprotectionv1.NewProvider(),
			),
		}
		if len(nodesOptions) == 1 {
			if opts, exists := nodesOptions[0][node.Name]; exists {
				baseOpts = append(baseOpts, opts...)
			}
		}

		node.Initialize(baseOpts...)

		if t.TransactionFramework == nil {
			t.TransactionFramework = NewTransactionFramework(node.Protocol, t.genesisSeed[:], t.optsAccounts...)
		}

		return true
	})

	t.running = true
}

func (t *TestSuite) Validators() []*mock.Node {
	validators := make([]*mock.Node, 0)
	t.nodes.ForEach(func(_ string, node *mock.Node) bool {
		if node.Validator {
			validators = append(validators, node)
		}

		return true
	})

	return validators
}

func (t *TestSuite) HookLogging() {
	t.mutex.RLock()
	defer t.mutex.RUnlock()

	t.nodes.ForEach(func(_ string, node *mock.Node) bool {
		node.HookLogging()
		return true
	})
}

// Eventually asserts that given condition will be met in opts.waitFor time,
// periodically checking target function each opts.tick.
//
//	assert.Eventually(t, func() bool { return true; }, time.Second, 10*time.Millisecond)
func (t *TestSuite) Eventually(condition func() error) {
	ch := make(chan error, 1)

	timer := time.NewTimer(t.optsWaitFor)
	defer timer.Stop()

	ticker := time.NewTicker(t.optsTick)
	defer ticker.Stop()

	var lastErr error
	for tick := ticker.C; ; {
		select {
		case <-timer.C:
			require.FailNow(t.Testing, "condition never satisfied", lastErr)
		case <-tick:
			tick = nil
			go func() { ch <- condition() }()
		case lastErr = <-ch:
			// The condition is satisfied, we can exit.
			if lastErr == nil {
				return
			}
			tick = ticker.C
		}
	}
}

func mustNodes(nodes []*mock.Node) {
	if len(nodes) == 0 {
		panic("no nodes provided")
	}
}
