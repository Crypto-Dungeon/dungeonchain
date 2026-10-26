package v13

import (
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

// UpgradeName is the on-chain plan name for the 2026-10-25 theft recovery.
const UpgradeName = "v13"

// Addresses involved in the 2026-10-25 hot-wallet theft (see the incident
// write-up in the operator notes). All three are plain base accounts.
const (
	// CompromisedHotWallet is the operator hot wallet whose key was stolen from
	// server 11 on 2026-10-25 ~06:00 UTC. Its liquid balance was swept by the
	// operator on 2026-10-26; what remains at upgrade time is its unbonding
	// delegations (9 entries, ~10.09M DGN, maturing 2026-11-14).
	CompromisedHotWallet = "dungeon13x4pynlp86prhcmtns742kgsgu7pjtzj72eycc"

	// ThiefAddress received 31,949,067.86 DGN from the hot wallet in three
	// MsgSends at heights 22314303 / 22314318 / 22314356. Fresh account,
	// never signed a transaction.
	ThiefAddress = "dungeon1q5sp3sl6h4kmgpt6ttrelrfgs07zsew039gs0f"

	// RecoveryAddress is the operator's new admin key, generated off-server on
	// 2026-10-26 and already holding the admin role of every operator contract.
	RecoveryAddress = "dungeon10cr0mweqjyz856kyyy03yzj6av3vmy0p0653lh"
)

// ForkHeight is the block at which the governance-free path runs Recover.
// 0 = disabled (BeginBlockForks never matches height 0). Set it to the agreed
// restart height before building the binary validators will run; every
// validator must build with the same value.
const ForkHeight int64 = 22368769

var Fork = upgrades.Fork{
	UpgradeName:    UpgradeName,
	UpgradeHeight:  ForkHeight,
	BeginForkLogic: RunForkLogic,
}

var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
}
