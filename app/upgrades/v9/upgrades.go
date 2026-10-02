package v9

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// MinCommissionRate is the network-wide validator commission floor (5%),
// matching the de-facto Cosmos ecosystem standard.
var MinCommissionRate = math.LegacyNewDecWithPrec(5, 2)

// CreateUpgradeHandler returns the v9 upgrade handler which:
//  1. Adds the IBC rate-limiting store (module InitGenesis runs via RunMigrations;
//     limits themselves are set post-upgrade by governance, per channel/denom).
//  2. Sets the staking MinCommissionRate param to 5% and raises any validator
//     currently below the floor.
//  3. Runs standard module migrations.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)

		if ak.StakingKeeper == nil {
			return nil, fmt.Errorf("v9 upgrade: StakingKeeper is nil")
		}

		// RunMigrations first: it InitGenesis-es the new ratelimit module and
		// migrates everything else before we touch staking state.
		vm, err := mm.RunMigrations(ctx, configurator, fromVM)
		if err != nil {
			return nil, err
		}

		logger.Info("v9: enforcing min commission floor", "floor", MinCommissionRate.String())

		params, err := ak.StakingKeeper.GetParams(ctx)
		if err != nil {
			return nil, err
		}
		params.MinCommissionRate = MinCommissionRate
		if err := ak.StakingKeeper.SetParams(ctx, params); err != nil {
			return nil, err
		}

		validators, err := ak.StakingKeeper.GetAllValidators(ctx)
		if err != nil {
			return nil, err
		}
		blockTime := sdkCtx.BlockHeader().Time
		for _, v := range validators {
			if v.Commission.Rate.GTE(MinCommissionRate) {
				continue
			}
			if v.Commission.MaxRate.LT(MinCommissionRate) {
				v.Commission.MaxRate = MinCommissionRate
			}
			v.Commission.Rate = MinCommissionRate
			v.Commission.UpdateTime = blockTime
			if err := ak.StakingKeeper.SetValidator(ctx, v); err != nil {
				return nil, err
			}
			logger.Info("v9: raised validator commission to floor", "validator", v.OperatorAddress)
		}

		logger.Info("v9: rate-limiting module added; min-commission floor applied")

		return vm, nil
	}
}
