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
	UpgradeHeight:  7193044,
	BeginForkLogic: RunForkLogic,
}
