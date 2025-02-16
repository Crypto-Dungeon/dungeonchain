package v4

import (
	storetypes "cosmossdk.io/store/types"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

const (
	UpgradeName = "v4"
)

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
	StoreUpgrades: storetypes.StoreUpgrades{
		Added: []string{},
		Deleted: []string{
			"ccvconsumer",
		},
	},
}
