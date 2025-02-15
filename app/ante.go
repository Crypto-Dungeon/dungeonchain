package app

import (
	"errors"

	"github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"

	"github.com/Crypto-Dungeon/dungeonchain/app/decorators"
	ibcante "github.com/cosmos/ibc-go/v8/modules/core/ante"
	"github.com/cosmos/ibc-go/v8/modules/core/keeper"

	corestoretypes "cosmossdk.io/core/store"
	circuitante "cosmossdk.io/x/circuit/ante"
	circuitkeeper "cosmossdk.io/x/circuit/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"

	globalfeeante "github.com/strangelove-ventures/globalfee/x/globalfee/ante"
	globalfeekeeper "github.com/strangelove-ventures/globalfee/x/globalfee/keeper"
)

// HandlerOptions extend the SDK's AnteHandler options by requiring the IBC
// channel keeper.
type HandlerOptions struct {
	ante.HandlerOptions

	IBCKeeper             *keeper.Keeper
	WasmConfig            *wasmtypes.WasmConfig
	WasmKeeper            *wasmkeeper.Keeper
	TXCounterStoreService corestoretypes.KVStoreService
	CircuitKeeper         *circuitkeeper.Keeper

	GlobalFeeKeeper      globalfeekeeper.Keeper
	BypassMinFeeMsgTypes []string
	// ConsumerKeeper       ccvconsumerkeeper.Keeper
}

// NewAnteHandler constructor
func NewAnteHandler(options HandlerOptions) (sdk.AnteHandler, error) {
	if options.AccountKeeper == nil {
		return nil, errors.New("account keeper is required for ante builder")
	}
	if options.BankKeeper == nil {
		return nil, errors.New("bank keeper is required for ante builder")
	}
	if options.SignModeHandler == nil {
		return nil, errors.New("sign mode handler is required for ante builder")
	}
	if options.WasmConfig == nil {
		return nil, errors.New("wasm config is required for ante builder")
	}
	if options.TXCounterStoreService == nil {
		return nil, errors.New("wasm store service is required for ante builder")
	}
	if options.CircuitKeeper == nil {
		return nil, errors.New("circuit keeper is required for ante builder")
	}

	anteDecorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(), // outermost AnteDecorator. SetUpContext must be called first
		// ccvconsumerante.NewMsgFilterDecorator(options.ConsumerKeeper),
		// ccvconsumerante.NewDisabledModulesDecorator("/cosmos.evidence", "/cosmos.slashing"),
		// ccvdemocracyante.NewForbiddenProposalsDecorator(IsLegacyProposalWhitelisted, IsModuleWhiteList),
		wasmkeeper.NewLimitSimulationGasDecorator(options.WasmConfig.SimulationGasLimit), // after setup context to enforce limits early
		wasmkeeper.NewCountTXDecorator(options.TXCounterStoreService),
		wasmkeeper.NewGasRegisterDecorator(options.WasmKeeper.GetGasRegister()),
		circuitante.NewCircuitBreakerDecorator(options.CircuitKeeper),
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		globalfeeante.NewFeeDecorator(options.BypassMinFeeMsgTypes, options.GlobalFeeKeeper, 2_000_000),
		//ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker),
		ante.NewSetPubKeyDecorator(options.AccountKeeper), // SetPubKeyDecorator must be called before all signature verification decorators
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		decorators.NewMsgStakingVestingDeny(options.AccountKeeper),
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
	}

	return sdk.ChainAnteDecorators(anteDecorators...), nil
}

var whiteListModule = map[string]struct{}{
	"/cosmos.gov.v1.MsgUpdateParams":                       {},
	"/cosmos.bank.v1beta1.MsgUpdateParams":                 {},
	"/cosmos.staking.v1beta1.MsgUpdateParams":              {},
	"/cosmos.distribution.v1beta1.MsgUpdateParams":         {},
	"/cosmos.mint.v1beta1.MsgUpdateParams":                 {},
	"/cosmos.gov.v1beta1.TextProposal":                     {},
	"/ibc.applications.transfer.v1.MsgUpdateParams":        {},
	"/interchain_security.ccv.consumer.v1.MsgUpdateParams": {},
	"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade":           {},
	"/cosmos.upgrade.v1beta1.MsgCancelUpgrade":             {},
}

func IsModuleWhiteList(typeUrl string) bool {
	_, found := whiteListModule[typeUrl]
	return found
}

func IsLegacyProposalWhitelisted(content v1beta1.Content) bool {
	return true
}
