package app

import (
	"context"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	v12 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v12"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	globalfeetypes "github.com/strangelove-ventures/globalfee/x/globalfee/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type v12MigrationFixture struct{ mutate func(context.Context) error }

func (m v12MigrationFixture) GetVersionMap() module.VersionMap {
	return module.VersionMap{"staking": 6}
}
func (m v12MigrationFixture) RunMigrations(ctx context.Context, _ module.Configurator, _ module.VersionMap) (module.VersionMap, error) {
	if m.mutate != nil {
		if err := m.mutate(ctx); err != nil {
			return nil, err
		}
	}
	return m.GetVersionMap(), nil
}
func TestV12ParameterGuards(t *testing.T) {
	for _, scenario := range []string{"unchanged", "staking", "consensus", "globalfee", "wrong-plan"} {
		t.Run(scenario, func(t *testing.T) {
			app := Setup(t)
			ctx := app.NewContextLegacy(false, cmtproto.Header{Height: 2, Time: time.Now().UTC()})
			fixture := v12MigrationFixture{mutate: func(c context.Context) error {
				switch scenario {
				case "staking":
					params, err := app.StakingKeeper.GetParams(c)
					if err != nil {
						return err
					}
					params.BondDenom = "different"
					return app.StakingKeeper.SetParams(c, params)
				case "consensus":
					params, err := app.ConsensusParamsKeeper.ParamsStore.Get(c)
					if err != nil {
						return err
					}
					params.Validator = &cmtproto.ValidatorParams{PubKeyTypes: []string{"ed25519", "ml_dsa_65"}}
					return app.ConsensusParamsKeeper.ParamsStore.Set(c, params)
				case "globalfee":
					app.GlobalFeeKeeper.SetParams(sdk.UnwrapSDKContext(c), globalfeetypes.Params{MinimumGasPrices: sdk.NewDecCoins(sdk.NewInt64DecCoin("stake", 1))})
				}
				return nil
			}}
			handler := v12.CreateUpgradeHandler(fixture, app.configurator, &upgrades.AppKeepers{
				AccountKeeper: &app.AccountKeeper, StakingKeeper: app.StakingKeeper,
				ConsensusParamsKeeper: &app.ConsensusParamsKeeper, GlobalFeeKeeper: &app.GlobalFeeKeeper,
			})
			name := v12.UpgradeName
			if scenario == "wrong-plan" {
				name = "v11"
			}
			result, err := handler(ctx, upgradetypes.Plan{Name: name, Height: 2}, module.VersionMap{"staking": 5})
			if scenario == "unchanged" {
				require.NoError(t, err)
				require.Equal(t, uint64(6), result["staking"])
				pool := app.AccountKeeper.GetModuleAccount(ctx, stakingtypes.KeyRotationFeePoolName)
				require.Contains(t, pool.GetPermissions(), "burner")
			} else {
				require.Error(t, err)
				require.Nil(t, result)
			}
		})
	}
}
