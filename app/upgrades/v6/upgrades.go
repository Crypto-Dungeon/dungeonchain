package v6

import (
	"context"
	"fmt"

	sdkmath "cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	globalfeetypes "github.com/strangelove-ventures/globalfee/x/globalfee/types"
)

// adminAddress is the Dungeon admin wallet that is exempt from minimum gas fees.
// This allows the admin to execute governance, upgrade, and operational transactions
// without needing to hold fee tokens during bootstrapping phases.
// The exemption is enforced in the ante chain (app/decorators/fee_exempt.go) at
// binary startup, not via on-chain state.
const adminAddress = "dungeon13x4pynlp86prhcmtns742kgsgu7pjtzj72eycc"

// minimumGasPrice* are the on-chain minimum gas price params set by v6.
// Prior to v6 the globalfee minimum was 0 (free transactions for all).
// After v6 non-exempt addresses must attach at least 0.01udgn per gas unit.
const minimumGasPriceDenom = "udgn"
const minimumGasPriceAmount = "0.010000000000000000"

// CreateUpgradeHandler returns the v6 upgrade handler which:
//  1. Sets x/globalfee MinimumGasPrices to 0.01udgn (enables fee enforcement).
//  2. Logs confirmation that admin fee exemption is wired in the binary ante chain.
//
// NOTE on ExemptAddresses: the globalfee Params proto struct has no
// ExemptAddresses field without a proto regeneration + store migration. The
// exemption is implemented as an in-process ante decorator (FeeExemptionAnteDecorator)
// initialized from ChainApp.FeeExemptAddresses at startup. No on-chain state needed.
//
// NOTE on LSM params: ValidatorBondFactor, GlobalLiquidStakingCap, and
// ValidatorLiquidStakingCap are NOT present in cosmos-sdk v0.50.13 vanilla.
// They live in the Cosmos Hub LSM fork of x/staking. Wire them here if/when
// dungeon-1 merges that fork. Tracked as TODO.
func CreateUpgradeHandler(
	mm upgrades.ModuleManager,
	configurator module.Configurator,
	ak *upgrades.AppKeepers,
) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		logger := sdkCtx.Logger().With("upgrade", UpgradeName)

		// 1. Set globalfee minimum gas price to 0.01udgn.
		if ak.GlobalFeeKeeper != nil {
			amount, err := sdkmath.LegacyNewDecFromStr(minimumGasPriceAmount)
			if err != nil {
				return nil, fmt.Errorf("v6 upgrade: invalid minimum gas price amount %q: %w", minimumGasPriceAmount, err)
			}
			newParams := globalfeetypes.Params{
				MinimumGasPrices: sdk.DecCoins{
					sdk.NewDecCoinFromDec(minimumGasPriceDenom, amount),
				},
			}
			if err := ak.GlobalFeeKeeper.SetParams(sdkCtx, newParams); err != nil {
				return nil, fmt.Errorf("v6 upgrade: failed to set globalfee params: %w", err)
			}
			logger.Info("globalfee minimum gas price set",
				"denom", minimumGasPriceDenom,
				"amount", minimumGasPriceAmount,
			)
		} else {
			logger.Error("GlobalFeeKeeper is nil — skipping globalfee param update")
		}

		// 2. Fee exemption for admin address is handled by FeeExemptionAnteDecorator,
		//    not on-chain state. Restart all validators with v6 binary — the exempt
		//    address is wired at app initialization in app.go.
		logger.Info("v6: admin fee exemption active via ante chain on v6 binary",
			"exempt_address", adminAddress,
		)

		// TODO(lee): LSM staking params
		// - ValidatorBondFactor    = 250
		// - GlobalLiquidStakingCap = "0.25"
		// - ValidatorLiquidStakingCap = "0.5"
		// Requires merging the Cosmos Hub LSM fork of x/staking into dungeonchain.
		// cosmos-sdk v0.50.13 vanilla does NOT include LSM.

		return mm.RunMigrations(ctx, configurator, fromVM)
	}
}
