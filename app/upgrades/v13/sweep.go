package v13

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

// SweepOperators empties the operator accounts of the rotated validators into
// RecoveryAddress: accrued validator commission, the rewards on their
// self-delegation, then every liquid coin. Their withdraw address is
// pointed back at the account first so payouts cannot land elsewhere. The
// self-delegation itself stays bonded (the account is frozen, so it cannot
// be unbonded). Runs after RotateKeys so the rotation fee is already spent.
// Safe to run twice.
func SweepOperators(ctx context.Context, ak *upgrades.AppKeepers, rotations []KeyRotation) error {
	logger := sdk.UnwrapSDKContext(ctx).Logger().With("upgrade", UpgradeName)
	b, err := ak.AccountKeeper.AddressCodec().StringToBytes(RecoveryAddress)
	if err != nil {
		return fmt.Errorf("v13: recovery address: %w", err)
	}
	recovery := sdk.AccAddress(b)

	for _, r := range rotations {
		valBz, err := ak.StakingKeeper.ValidatorAddressCodec().StringToBytes(r.Validator)
		if err != nil {
			return fmt.Errorf("v13: validator %s: %w", r.Validator, err)
		}
		val := sdk.ValAddress(valBz)
		op := sdk.AccAddress(valBz)

		if err := ak.DistrKeeper.SetWithdrawAddr(ctx, op, op); err != nil && !errors.Is(err, distrtypes.ErrSetWithdrawAddrDisabled) {
			return fmt.Errorf("v13: reset withdraw address of %s: %w", op, err)
		}
		if _, err := ak.DistrKeeper.WithdrawValidatorCommission(ctx, val); err != nil && !errors.Is(err, distrtypes.ErrNoValidatorCommission) {
			return fmt.Errorf("v13: withdraw commission of %s: %w", r.Validator, err)
		}
		if _, err := ak.StakingKeeper.GetDelegation(ctx, op, val); err == nil {
			if _, err := ak.DistrKeeper.WithdrawDelegationRewards(ctx, op, val); err != nil {
				return fmt.Errorf("v13: withdraw self-delegation rewards of %s: %w", r.Validator, err)
			}
		}

		coins := ak.BankKeeper.GetAllBalances(ctx, op)
		if coins.IsZero() {
			continue
		}
		if err := ak.BankKeeper.SendCoins(ctx, op, recovery, coins); err != nil {
			return fmt.Errorf("v13: sweep %s: %w", op, err)
		}
		if left := ak.BankKeeper.GetAllBalances(ctx, op); !left.IsZero() {
			return fmt.Errorf("v13: %s still holds %s after sweep", op, left)
		}
		logger.Info("v13: swept operator account", "validator", r.Validator, "coins", coins.String())
	}
	return nil
}
