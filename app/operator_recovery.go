package app

import (
	"github.com/Crypto-Dungeon/dungeonchain/app/decorators"
	"github.com/Crypto-Dungeon/dungeonchain/app/operatorrecovery"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

func (app *ChainApp) operatorRecoveryConfigurator() module.Configurator {
	return decorators.WrapOperatorRecoveryConfigurator(app.configurator, app.AppCodec(), *app.operatorRecoveryRule())
}

func (app *ChainApp) operatorRecoveryRule() *decorators.FrozenOperatorRecovery {
	op, err := app.AccountKeeper.AddressCodec().StringToBytes(operatorrecovery.Operator)
	if err != nil {
		panic(err)
	}
	controller, err := app.AccountKeeper.AddressCodec().StringToBytes(operatorrecovery.Controller)
	if err != nil {
		panic(err)
	}
	return &decorators.FrozenOperatorRecovery{
		Operator: op, Controller: controller, Allows: operatorrecovery.Allows,
		Active: func(ctx sdk.Context) (bool, error) {
			// This read-only activation probe must not change pre-activation
			// transaction gas/results compared with the existing frozen gate.
			height, err := app.UpgradeKeeper.GetDoneHeight(ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()), operatorrecovery.Name)
			return height > 0 && ctx.BlockHeight() >= height, err
		},
	}
}
