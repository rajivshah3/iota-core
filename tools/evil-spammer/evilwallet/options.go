package evilwallet

import (
	"time"

	"github.com/iotaledger/hive.go/ds/types"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/iota-core/tools/evil-spammer/models"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/builder"
)

// region Options ///////////////////////////////////////////////////////////////////////////

// Options is a struct that represents a collection of options that can be set when creating a block.
type Options struct {
	aliasInputs        map[string]types.Empty
	inputs             []*models.Output
	aliasOutputs       map[string]iotago.Output
	outputs            []iotago.Output
	inputWallet        *Wallet
	outputWallet       *Wallet
	outputBatchAliases map[string]types.Empty
	reuse              bool
	issuingTime        time.Time
	allotmentStrategy  models.AllotmentStrategy
	issuerAccountID    iotago.AccountID
	// maps input alias to desired output type, used to create account output types
	specialOutputTypes map[string]iotago.OutputType
}

type OutputOption struct {
	aliasName  string
	amount     iotago.BaseToken
	address    *iotago.Ed25519Address
	outputType iotago.OutputType
}

// NewOptions is the constructor for the tx creation.
func NewOptions(options ...Option) (option *Options, err error) {
	option = &Options{
		aliasInputs:        make(map[string]types.Empty),
		inputs:             make([]*models.Output, 0),
		aliasOutputs:       make(map[string]iotago.Output),
		outputs:            make([]iotago.Output, 0),
		specialOutputTypes: make(map[string]iotago.OutputType),
	}

	for _, opt := range options {
		opt(option)
	}

	// check if alias and non-alias are mixed in use.
	if err := option.checkInputsAndOutputs(); err != nil {
		return nil, err
	}

	// input and output wallets must be provided if inputs/outputs are not aliases.
	if err := option.isWalletProvidedForInputsOutputs(); err != nil {
		return nil, err
	}

	if option.outputWallet == nil {
		option.outputWallet = NewWallet()
	}

	return
}

// Option is the type that is used for options that can be passed into the CreateBlock method to configure its
// behavior.
type Option func(*Options)

func (o *Options) isBalanceProvided() bool {
	provided := false

	for _, output := range o.aliasOutputs {
		if output.BaseTokenAmount() > 0 {
			provided = true
		}
	}

	return provided
}

func (o *Options) isWalletProvidedForInputsOutputs() error {
	if o.areInputsProvidedWithoutAliases() {
		if o.inputWallet == nil {
			return ierrors.New("no input wallet provided for inputs without aliases")
		}
	}
	if o.areOutputsProvidedWithoutAliases() {
		if o.outputWallet == nil {
			return ierrors.New("no output wallet provided for outputs without aliases")
		}
	}

	return nil
}

func (o *Options) areInputsProvidedWithoutAliases() bool {
	return len(o.inputs) > 0
}

func (o *Options) areOutputsProvidedWithoutAliases() bool {
	return len(o.outputs) > 0
}

// checkInputsAndOutputs checks if either all provided inputs/outputs are with aliases or all are without,
// we do not allow for mixing those two possibilities.
func (o *Options) checkInputsAndOutputs() error {
	inLength, outLength, aliasInLength, aliasOutLength := len(o.inputs), len(o.outputs), len(o.aliasInputs), len(o.aliasOutputs)

	if (inLength == 0 && aliasInLength == 0) || (outLength == 0 && aliasOutLength == 0) {
		return ierrors.New("no inputs or outputs provided")
	}

	inputsOk := (inLength > 0 && aliasInLength == 0) || (aliasInLength > 0 && inLength == 0)
	outputsOk := (outLength > 0 && aliasOutLength == 0) || (aliasOutLength > 0 && outLength == 0)
	if !inputsOk || !outputsOk {
		return ierrors.New("mixing providing inputs/outputs with and without aliases is not allowed")
	}

	return nil
}

// WithInputs returns an Option that is used to provide the Inputs of the Transaction.
func WithInputs(inputs interface{}) Option {
	return func(options *Options) {
		switch in := inputs.(type) {
		case string:
			options.aliasInputs[in] = types.Void
		case []string:
			for _, input := range in {
				options.aliasInputs[input] = types.Void
			}
		case *models.Output:
			options.inputs = append(options.inputs, in)
		case []*models.Output:
			options.inputs = append(options.inputs, in...)
		}
	}
}

// WithOutputs returns an Option that is used to define a non-colored Outputs for the Transaction in the Block.
func WithOutputs(outputsOptions []*OutputOption) Option {
	return func(options *Options) {
		for _, outputOptions := range outputsOptions {
			var output iotago.Output
			switch outputOptions.outputType {
			case iotago.OutputBasic:
				outputBuilder := builder.NewBasicOutputBuilder(outputOptions.address, outputOptions.amount)
				output = outputBuilder.MustBuild()
			case iotago.OutputAccount:
				outputBuilder := builder.NewAccountOutputBuilder(outputOptions.address, outputOptions.address, outputOptions.amount)
				output = outputBuilder.MustBuild()
			}

			if outputOptions.aliasName != "" {
				options.aliasOutputs[outputOptions.aliasName] = output
			} else {
				options.outputs = append(options.outputs, output)
			}
		}
	}
}

func WithIssuanceStrategy(strategy models.AllotmentStrategy, issuerID iotago.AccountID) Option {
	return func(options *Options) {
		options.allotmentStrategy = strategy
		options.issuerAccountID = issuerID
	}
}

// WithInputWallet returns a BlockOption that is used to define the inputWallet of the Block.
func WithInputWallet(issuer *Wallet) Option {
	return func(options *Options) {
		options.inputWallet = issuer
	}
}

// WithOutputWallet returns a BlockOption that is used to define the inputWallet of the Block.
func WithOutputWallet(wallet *Wallet) Option {
	return func(options *Options) {
		options.outputWallet = wallet
	}
}

// WithOutputBatchAliases returns a BlockOption that is used to determine which outputs should be added to the outWallet.
func WithOutputBatchAliases(outputAliases map[string]types.Empty) Option {
	return func(options *Options) {
		options.outputBatchAliases = outputAliases
	}
}

// WithReuseOutputs returns a BlockOption that is used to enable deep spamming with Reuse wallet outputs.
func WithReuseOutputs() Option {
	return func(options *Options) {
		options.reuse = true
	}
}

// WithIssuingTime returns a BlockOption that is used to set issuing time of the Block.
func WithIssuingTime(issuingTime time.Time) Option {
	return func(options *Options) {
		options.issuingTime = issuingTime
	}
}

// ConflictSlice represents a set of conflict transactions.
type ConflictSlice [][]Option

// endregion  //////////////////////////////////////////////////////////////////////////////////////////////////////////

// region FaucetRequestOptions /////////////////////////////////////////////////////////////////////////////////////////

// FaucetRequestOptions is options for faucet request.
type FaucetRequestOptions struct {
	outputAliasName string
}

// NewFaucetRequestOptions creates options for a faucet request.
func NewFaucetRequestOptions(options ...FaucetRequestOption) *FaucetRequestOptions {
	reqOptions := &FaucetRequestOptions{
		outputAliasName: "",
	}

	for _, option := range options {
		option(reqOptions)
	}

	return reqOptions
}

// FaucetRequestOption is an option for faucet request.
type FaucetRequestOption func(*FaucetRequestOptions)

// WithOutputAlias returns an Option that is used to provide the Output of the Transaction.
func WithOutputAlias(aliasName string) FaucetRequestOption {
	return func(options *FaucetRequestOptions) {
		options.outputAliasName = aliasName
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region EvilScenario Options /////////////////////////////////////////////////////////////////////////////////////////

type ScenarioOption func(scenario *EvilScenario)

// WithScenarioCustomConflicts specifies the EvilBatch that describes the UTXO structure that should be used for the spam.
func WithScenarioCustomConflicts(batch EvilBatch) ScenarioOption {
	return func(options *EvilScenario) {
		if batch != nil {
			options.ConflictBatch = batch
		}
	}
}

// WithScenarioDeepSpamEnabled enables deep spam, the outputs from available Reuse wallets or RestrictedReuse wallet
// if provided with WithReuseInputWalletForDeepSpam option will be used for spam instead fresh faucet outputs.
func WithScenarioDeepSpamEnabled() ScenarioOption {
	return func(options *EvilScenario) {
		options.Reuse = true
	}
}

// WithScenarioReuseOutputWallet the outputs from the spam will be saved into this wallet, accepted types of wallet: Reuse, RestrictedReuse.
func WithScenarioReuseOutputWallet(wallet *Wallet) ScenarioOption {
	return func(options *EvilScenario) {
		if wallet != nil {
			if wallet.walletType == Reuse || wallet.walletType == RestrictedReuse {
				options.OutputWallet = wallet
			}
		}
	}
}

// WithScenarioInputWalletForDeepSpam reuse set to true, outputs from this wallet will be used for deep spamming,
// allows for controllable building of UTXO deep structures. Accepts only RestrictedReuse wallet type.
func WithScenarioInputWalletForDeepSpam(wallet *Wallet) ScenarioOption {
	return func(options *EvilScenario) {
		if wallet.walletType == RestrictedReuse {
			options.RestrictedInputWallet = wallet
		}
	}
}

func WithCreateAccounts() ScenarioOption {
	return func(options *EvilScenario) {
		options.OutputType = iotago.OutputAccount
	}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
