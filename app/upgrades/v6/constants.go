package v6

import (
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
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
