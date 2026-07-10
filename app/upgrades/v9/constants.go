package v9

import (
	storetypes "cosmossdk.io/store/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	ratelimittypes "github.com/cosmos/ibc-apps/modules/rate-limiting/v10/ratelimit/types"
)

const (
	// UpgradeName defines the on-chain upgrade name for governance proposal.
	UpgradeName = "v9"
)

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
	StoreUpgrades: storetypes.StoreUpgrades{
		Added: []string{
			ratelimittypes.StoreKey,
		},
		Deleted: []string{},
	},
}
