package v9

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	ratelimittypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
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
