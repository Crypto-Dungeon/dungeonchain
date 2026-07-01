package app

import (
	"fmt"
	v4 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v4"
	v5 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v5"
	v6 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v6"
	v7 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v7"
	v8 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v8"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades/noop"
)

// Upgrades list of chain upgrades
var Upgrades = []upgrades.Upgrade{v5.Upgrade, v6.Upgrade, v7.Upgrade, v8.Upgrade}
var Forks = []upgrades.Fork{v4.Upgrade}

// RegisterUpgradeHandlers registers the chain upgrade handlers
func (app *ChainApp) RegisterUpgradeHandlers() {
	// setupLegacyKeyTables(&app.ParamsKeeper)
	if len(Upgrades) == 0 {
		// always have a unique upgrade registered for the current version to test in system tests
		Upgrades = append(Upgrades, noop.NewUpgrade(app.Version()))
	}

	keepers := upgrades.AppKeepers{
		AccountKeeper:         &app.AccountKeeper,
		BankKeeper:            &app.BankKeeper,
		ParamsKeeper:          &app.ParamsKeeper,
		ConsensusParamsKeeper: &app.ConsensusParamsKeeper,
		CapabilityKeeper:      app.CapabilityKeeper,
		IBCKeeper:             app.IBCKeeper,
		Codec:                 app.appCodec,
		GetStoreKey:           app.GetKey,
		GlobalFeeKeeper:       &app.GlobalFeeKeeper,
		HyperlaneKeeper:       app.HyperlaneKeeper,
		WarpKeeper:            &app.WarpKeeper,
	}
	app.GetStoreKeys()
	// register all upgrade handlers
	for _, upgrade := range Upgrades {
		app.UpgradeKeeper.SetUpgradeHandler(
			upgrade.UpgradeName,
			upgrade.CreateUpgradeHandler(
				app.ModuleManager,
				app.configurator,
				&keepers,
			),
		)
	}

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Sprintf("failed to read upgrade info from disk %s", err))
	}

	if app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		return
	}

	// register store loader for current upgrade
	for _, upgrade := range Upgrades {
		if upgradeInfo.Name == upgrade.UpgradeName {
			app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &upgrade.StoreUpgrades)) // nolint:gosec
			break
		}
	}
}
