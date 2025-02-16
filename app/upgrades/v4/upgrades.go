package v4

import (
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func RunForkLogic(ctx sdk.Context, ak *upgrades.AppKeepers) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.Logger().Info("Starting fork logic...")
}
