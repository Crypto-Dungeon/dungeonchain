package v12

import (
	"context"
	"fmt"
	"reflect"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

const UpgradeName = "v12"

// V12 migrates the live SDK53 state directly to SDK55. Existing consensus
// key allowances are preserved; enabling ML-DSA is a separate coordinated
// consensus-parameter update, followed by validator rotations.
var Upgrade = upgrades.Upgrade{
	UpgradeName:          UpgradeName,
	CreateUpgradeHandler: CreateUpgradeHandler,
}

func CreateUpgradeHandler(mm upgrades.ModuleManager, cfg module.Configurator, ak *upgrades.AppKeepers) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		if plan.Name != UpgradeName {
			return nil, fmt.Errorf("expected %s plan, got %s", UpgradeName, plan.Name)
		}
		paramsBefore, err := ak.StakingKeeper.GetParams(ctx)
		if err != nil {
			return nil, err
		}
		consensusBefore, err := ak.ConsensusParamsKeeper.ParamsStore.Get(ctx)
		if err != nil {
			return nil, err
		}
		feesBefore := ak.GlobalFeeKeeper.GetParams(sdk.UnwrapSDKContext(ctx))
		// Materialize the mandatory burner account before the staking migrations.
		if ak.AccountKeeper.GetModuleAccount(ctx, stakingtypes.KeyRotationFeePoolName) == nil {
			return nil, fmt.Errorf("missing key-rotation fee pool")
		}
		migrated, err := mm.RunMigrations(ctx, cfg, fromVM)
		if err != nil {
			return nil, err
		}
		paramsAfter, err := ak.StakingKeeper.GetParams(ctx)
		if err != nil {
			return nil, err
		}
		if !paramsBefore.MinCommissionRate.Equal(paramsAfter.MinCommissionRate) || paramsBefore.BondDenom != paramsAfter.BondDenom {
			return nil, fmt.Errorf("staking migration changed Dungeon commission floor or bond denom")
		}
		consensusAfter, err := ak.ConsensusParamsKeeper.ParamsStore.Get(ctx)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(consensusBefore.GetValidator().GetPubKeyTypes(), consensusAfter.GetValidator().GetPubKeyTypes()) {
			return nil, fmt.Errorf("migration changed Dungeon consensus key allowance")
		}
		feesAfter := ak.GlobalFeeKeeper.GetParams(sdk.UnwrapSDKContext(ctx))
		if !reflect.DeepEqual(feesBefore, feesAfter) {
			return nil, fmt.Errorf("migration changed Dungeon global fee policy")
		}
		return migrated, nil
	}
}
