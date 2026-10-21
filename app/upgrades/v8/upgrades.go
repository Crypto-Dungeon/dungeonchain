package v8

import (
	"context"
	"fmt"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	hyperlane "github.com/bcp-innovations/hyperlane-cosmos/x/core"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warp "github.com/bcp-innovations/hyperlane-cosmos/x/warp"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// CreateUpgradeHandler returns the v8 upgrade handler which:
//  1. Adds Hyperlane core and warp route stores.
//  2. Initializes default Hyperlane genesis state for mailbox, ISM, post-dispatch,
//     and warp token state.
//  3. Runs standard module migrations.
//
// v8 intentionally enables only Hyperlane collateral tokens in the binary. This
// makes dungeon-1 the canonical DGN side of the bridge while avoiding synthetic
// mint/burn behavior until a later governance-approved upgrade.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)

		if ak.HyperlaneKeeper == nil {
			return nil, fmt.Errorf("v8 upgrade: HyperlaneKeeper is nil")
		}
		if ak.WarpKeeper == nil {
			return nil, fmt.Errorf("v8 upgrade: WarpKeeper is nil")
		}

		logger.Info("v8: initializing Hyperlane core module")
		hyperlane.NewAppModule(ak.Codec, ak.HyperlaneKeeper).InitGenesis(
			sdkCtx,
			ak.Codec,
			ak.Codec.MustMarshalJSON(hyperlanetypes.NewGenesisState()),
		)

		logger.Info("v8: initializing Hyperlane warp module", "enabled_token_type", warptypes.HYP_TOKEN_TYPE_COLLATERAL.String())
		warp.NewAppModule(ak.Codec, *ak.WarpKeeper).InitGenesis(
			sdkCtx,
			ak.Codec,
			ak.Codec.MustMarshalJSON(warptypes.NewGenesisState()),
		)

		vm, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, err
		}

		logger.Info("v8: Hyperlane bridge hub modules initialized")

		return vm, nil
	}
}
