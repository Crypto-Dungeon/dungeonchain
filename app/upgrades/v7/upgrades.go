package v7

import (
	"context"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// CreateUpgradeHandler returns the v7 upgrade handler which performs:
//
//  1. RunMigrations -- picks up all module consensus version bumps:
//     - ibc-go core: v6 -> v8 (two-step internal migration)
//     - ibc-go transfer: v5 -> v6
//     - wasmd: unchanged at v4 (no state migration needed)
//     - cosmos-sdk core modules: unchanged (no consensus version bumps in v0.50->v0.53)
//
//  2. feeibc store deletion -- handled by StoreUpgrades.Deleted in constants.go.
//     The 29-fee (ICS-29) module was removed in ibc-go v10. The store loader
//     registered in RegisterUpgradeHandlers will prune it at upgrade height.
//
//  3. cosmwasm_2_0 capability is enabled by the v7 binary AllCapabilities()
//     in app/wasm.go -- no on-chain state change needed. Contracts compiled with
//     bulk-memory will become deployable on dungeon-1 once this upgrade lands.
//     This unblocks the BTC SPV bridge (code_id 109, 39 tests passing).
//
// NOTE: ibcfee module params/state are non-recoverable after this upgrade.
// dungeon-1 had ibcfee enabled in app but no active fee-enabled channels,
// so no user funds are locked in the fee module store.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)

		logger.Info("v7: starting SDK v0.53 + wasmd v0.60 + ibc-go v10 migration")

		// RunMigrations handles all module consensus version bumps automatically.
		// ibc-go v10 registers its own migration handlers for core (6->7->8) and
		// transfer (5->6) -- these run inside RunMigrations.
		vm, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, err
		}

		logger.Info("v7: RunMigrations complete -- ibc-go v10 + SDK v0.53 migrations applied")
		logger.Info("v7: cosmwasm_2_0 capability active -- bulk-memory contracts now deployable")
		logger.Info("v7: feeibc store pruned by store loader at this height")

		return vm, nil
	}
}