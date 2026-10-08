package testutil

import (
	appv1alpha1 "cosmossdk.io/api/cosmos/app/v1alpha1"
	"cosmossdk.io/core/appconfig"
	groupmodulev1 "github.com/Crypto-Dungeon/dungeonchain/compatapi/group/module/v1"
	paramsmodulev1 "github.com/Crypto-Dungeon/dungeonchain/compatapi/params/module/v1"
	"github.com/cosmos/cosmos-sdk/testutil/configurator"
	_ "github.com/cosmos/cosmos-sdk/x/auth"           // import as blank for app wiring
	_ "github.com/cosmos/cosmos-sdk/x/auth/tx/config" // import as blank for app wiring
	_ "github.com/cosmos/cosmos-sdk/x/authz"          // import as blank for app wiring
	_ "github.com/cosmos/cosmos-sdk/x/bank"           // import as blank for app wiring
	_ "github.com/cosmos/cosmos-sdk/x/consensus"      // import as blank for app wiring
	_ "github.com/cosmos/cosmos-sdk/x/genutil"        // import as blank for app wiring
	_ "github.com/cosmos/cosmos-sdk/x/group/module"   // import as blank for app wiring
	_ "github.com/cosmos/cosmos-sdk/x/mint"           // import as blank for app wiring
	_ "github.com/cosmos/cosmos-sdk/x/params"         // import as blank for app wiring
	_ "github.com/cosmos/cosmos-sdk/x/staking"        // import as blank for app wiring
)

var AppConfig = configurator.NewAppConfig(
	configurator.AuthModule(),
	configurator.BankModule(),
	configurator.StakingModule(),
	configurator.TxModule(),
	configurator.ConsensusModule(),
	paramsModule(),
	configurator.GenutilModule(),
	groupModule(),
)

func paramsModule() configurator.ModuleOption {
	return func(config *configurator.Config) {
		config.ModuleConfigs["params"] = &appv1alpha1.ModuleConfig{Name: "params", Config: appconfig.WrapAny(&paramsmodulev1.Module{})}
	}
}
func groupModule() configurator.ModuleOption {
	return func(config *configurator.Config) {
		config.ModuleConfigs["group"] = &appv1alpha1.ModuleConfig{Name: "group", Config: appconfig.WrapAny(&groupmodulev1.Module{})}
	}
}
