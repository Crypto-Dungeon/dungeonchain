package v6

import (
	storetypes "cosmossdk.io/store/types"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

const (
	// UpgradeName defines the on-chain upgrade name for governance proposal.
	UpgradeName = "v6"
)

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
	StoreUpgrades: storetypes.StoreUpgrades{
		Added:   []string{},
		Deleted: []string{},
	},
}
