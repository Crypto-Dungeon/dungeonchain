package v7

import (
	storetypes "cosmossdk.io/store/types"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

const (
	// UpgradeName defines the on-chain upgrade name for governance proposal.
	UpgradeName = "v7"
)

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
	StoreUpgrades: storetypes.StoreUpgrades{
		Added: []string{},
		// feeibc store is deleted: ibc-go v10 dropped the 29-fee (ICS-29) module entirely.
		Deleted: []string{"feeibc"},
	},
}
