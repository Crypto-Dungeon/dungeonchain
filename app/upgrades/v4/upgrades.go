package v4

import (
	storetypes "cosmossdk.io/store/types"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RunForkLogic(ctx sdk.Context, ak *upgrades.AppKeepers) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.Logger().Info("Starting fork logic...")

	// Get the module version map
	vm, err := ak.UpgradeKeeper.GetModuleVersionMap(ctx)
	if err != nil {
		panic("Failed to get module version map: " + err.Error())
	}

	// Remove "ccvconsumer" from the module version map
	if _, exists := vm["ccvconsumer"]; exists {
		delete(vm, "ccvconsumer")
		sdkCtx.Logger().Info("ccvconsumer module removed from module version map.")
	}

	// Apply store upgrade to delete "ccvconsumer"
	storeUpgrades := storetypes.StoreUpgrades{
		Deleted: []string{"ccvconsumer"},
	}

	err = ak.CommitMultiStore.LoadLatestVersionAndUpgrade(&storeUpgrades)
	if err != nil {
		panic("Failed to apply store upgrade: " + err.Error())
	}

	sdkCtx.Logger().Info("Fork logic executed successfully.")
}
