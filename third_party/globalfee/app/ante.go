package app

import (
	"errors"

	ibcante "github.com/cosmos/ibc-go/v8/modules/core/ante"
	"github.com/cosmos/ibc-go/v8/modules/core/keeper"

	circuitante "github.com/cosmos/cosmos-sdk/contrib/x/circuit/ante"
	circuitkeeper "github.com/cosmos/cosmos-sdk/contrib/x/circuit/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	globalfeeante "github.com/strangelove-ventures/globalfee/x/globalfee/ante"
	globalfeekeeper "github.com/strangelove-ventures/globalfee/x/globalfee/keeper"
)

const maxBypassMinFeeMsgGasUsage = 2_000_000

// HandlerOptions extend the SDK's AnteHandler options by requiring the IBC
// channel keeper.
type HandlerOptions struct {
	ante.HandlerOptions

	IBCKeeper     *keeper.Keeper
	CircuitKeeper *circuitkeeper.Keeper

	GlobalFeeKeeper      globalfeekeeper.Keeper
	BypassMinFeeMsgTypes []string
	StakingKeeper        *stakingkeeper.Keeper // TODO: save the bond denom instead

}

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
	if options.CircuitKeeper == nil {
		return nil, errors.New("circuit keeper is required for ante builder")
	}

	anteDecorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(),
		circuitante.NewCircuitBreakerDecorator(options.CircuitKeeper),
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		// ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker),
		globalfeeante.NewFeeDecorator(options.BypassMinFeeMsgTypes, options.GlobalFeeKeeper, maxBypassMinFeeMsgGasUsage),
		ante.NewSetPubKeyDecorator(options.AccountKeeper), // SetPubKeyDecorator must be called before all signature verification decorators
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
	}

	return sdk.ChainAnteDecorators(anteDecorators...), nil
}
