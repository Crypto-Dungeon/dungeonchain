package v4

import (
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

const (
	// UpgradeName defines the on-chain upgrade name.
	UpgradeName = "v4"
)

var Upgrade = upgrades.Fork{
	UpgradeName:    UpgradeName,
	UpgradeHeight:  10,
	BeginForkLogic: RunForkLogic,
}

//var Upgrade = upgrades.Upgrade{
//	UpgradeName:          UpgradeName,
//	CreateUpgradeHandler: CreateUpgradeHandler,
//	StoreUpgrades: storetypes.StoreUpgrades{
//		Added: []string{},
//		Deleted: []string{
//			"ccvconsumer",
//		},
//	},
//}
