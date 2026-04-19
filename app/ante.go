package app

import (
	"errors"

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
	// ExemptAddresses is the list of bech32 addresses that pay zero fees.
	// Populated from the v6 upgrade params; defaults to empty at genesis.
	ExemptAddresses []string
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

	// postFeeDecorators is everything that runs AFTER fee checking.
	// We need this as a standalone handler so that the exemption decorator
	// can jump directly to it, skipping globalfeeante.FeeDecorator.
	postFeeDecorators := []sdk.AnteDecorator{
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		decorators.NewMsgStakingVestingDeny(options.AccountKeeper),
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
	}
	postFeeHandler := sdk.ChainAnteDecorators(postFeeDecorators...)

	// Full ante chain. FeeExemptionAnteDecorator sits immediately before
	// globalfeeante.FeeDecorator. Exempt signers jump to postFeeHandler;
	// everyone else falls through to FeeDecorator then postFeeDecorators
	// (which are also appended below so non-exempt paths remain correct).
	anteDecorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(),
		wasmkeeper.NewLimitSimulationGasDecorator(options.WasmConfig.SimulationGasLimit),
		wasmkeeper.NewCountTXDecorator(options.TXCounterStoreService),
		wasmkeeper.NewGasRegisterDecorator(options.WasmKeeper.GetGasRegister()),
		circuitante.NewCircuitBreakerDecorator(options.CircuitKeeper),
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		// Fee exemption MUST come before globalfee FeeDecorator.
		decorators.NewFeeExemptionAnteDecorator(options.ExemptAddresses, postFeeHandler),
		globalfeeante.NewFeeDecorator(options.BypassMinFeeMsgTypes, options.GlobalFeeKeeper, 2_000_000),
		// Post-fee decorators run for non-exempt paths (exempt paths use postFeeHandler above).
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		decorators.NewMsgStakingVestingDeny(options.AccountKeeper),
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
	}

	return sdk.ChainAnteDecorators(anteDecorators...), nil
}
