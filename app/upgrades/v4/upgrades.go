package v4

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		sdkCtx.Logger().Info("Starting upgrade handler")
		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
