package v8

import (
	storetypes "cosmossdk.io/store/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
)

const (
	// UpgradeName defines the on-chain upgrade name for governance proposal.
	UpgradeName = "v8"
)

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
	StoreUpgrades: storetypes.StoreUpgrades{
		Added: []string{
			hyperlanetypes.ModuleName,
			warptypes.ModuleName,
		},
		Deleted: []string{},
	},
}
