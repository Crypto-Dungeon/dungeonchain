package v13

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// CreateUpgradeHandler returns the v13 handler: recovery of the 2026-10-25
// hot-wallet theft. It is a pure state correction, no module migrations and no
// dependency changes:
//
//  1. Every coin held by ThiefAddress moves to RecoveryAddress.
//  2. Every unbonding delegation of CompromisedHotWallet is removed and its
//     remaining balance paid out of the not-bonded pool to RecoveryAddress,
//     so the 2026-11-14 maturity never lands in the stolen wallet.
//  3. Anything still sitting liquid in CompromisedHotWallet moves to
//     RecoveryAddress as well.
//  4. The three operator validators rotate to fresh consensus keys
//     (rotate.go), fee paid from RecoveryAddress.
//  5. Their operator accounts' commission, self-delegation rewards and
//     liquid balance move to RecoveryAddress (sweep.go); from ForkHeight on
//     the ante handler refuses every tx those accounts, the hot wallet or
//     the thief sign (decorators/frozen.go).
//  6. Contract roles still held by the hot wallet (cw721 minter-owners, CLOB
//     admins) move to RecoveryAddress (contracts.go).
//
// Amounts are read from state at upgrade height, never hardcoded, so the
// handler is correct whatever moves between now and the halt. It refuses to
// run under any other plan name and fails loudly if the recovery address does
// not end up holding what was taken out.
func CreateUpgradeHandler(mm upgrades.ModuleManager, cfg module.Configurator, ak *upgrades.AppKeepers) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		if plan.Name != UpgradeName {
			return nil, fmt.Errorf("expected %s plan, got %s", UpgradeName, plan.Name)
		}
		if err := Recover(ctx, ak); err != nil {
			return nil, err
		}
		if err := RotateKeys(ctx, ak, Rotations); err != nil {
			return nil, err
		}
		if err := SweepOperators(ctx, ak, Rotations); err != nil {
			return nil, err
		}
		if err := TransferContractRoles(ctx, ak, RoleTransfers); err != nil {
			return nil, err
		}
		return mm.RunMigrations(ctx, cfg, fromVM)
	}
}

// RunForkLogic is the governance-free path: validators restart on this binary
// before UpgradeHeight (constants.go) and BeginBlock at that height runs the
// same recovery. A failure here panics on purpose: a half-applied recovery
// must halt the chain, not slip into consensus.
func RunForkLogic(ctx sdk.Context, ak *upgrades.AppKeepers) {
	if err := Recover(ctx, ak); err != nil {
		panic(fmt.Errorf("v13 fork: %w", err))
	}
	if err := RotateKeys(ctx, ak, Rotations); err != nil {
		panic(fmt.Errorf("v13 fork: %w", err))
	}
	if err := SweepOperators(ctx, ak, Rotations); err != nil {
		panic(fmt.Errorf("v13 fork: %w", err))
	}
	if err := TransferContractRoles(ctx, ak, RoleTransfers); err != nil {
		panic(fmt.Errorf("v13 fork: %w", err))
	}
}

// Recover performs the state correction. Safe to run twice: a second run finds
// nothing to move and changes nothing.
func Recover(ctx context.Context, ak *upgrades.AppKeepers) error {
	{
		logger := sdk.UnwrapSDKContext(ctx).Logger().With("upgrade", UpgradeName)

		// Decode through the account keeper's codec, not the process-global
		// bech32 config, so the handler behaves the same in tests and in the node.
		addr := func(bech string) (sdk.AccAddress, error) {
			b, err := ak.AccountKeeper.AddressCodec().StringToBytes(bech)
			return sdk.AccAddress(b), err
		}
		thief, err := addr(ThiefAddress)
		if err != nil {
			return fmt.Errorf("v13: thief address: %w", err)
		}
		hot, err := addr(CompromisedHotWallet)
		if err != nil {
			return fmt.Errorf("v13: hot wallet address: %w", err)
		}
		recovery, err := addr(RecoveryAddress)
		if err != nil {
			return fmt.Errorf("v13: recovery address: %w", err)
		}

		before := ak.BankKeeper.GetAllBalances(ctx, recovery)
		expected := sdk.NewCoins()

		// 1 + 3: liquid balances.
		for _, from := range []sdk.AccAddress{thief, hot} {
			coins := ak.BankKeeper.GetAllBalances(ctx, from)
			if coins.IsZero() {
				continue
			}
			if err := ak.BankKeeper.SendCoins(ctx, from, recovery, coins); err != nil {
				return fmt.Errorf("v13: move %s from %s: %w", coins, from, err)
			}
			expected = expected.Add(coins...)
			logger.Info("v13: moved liquid balance", "from", from.String(), "coins", coins.String())
		}

		// 2: unbonding delegations of the compromised wallet.
		bondDenom, err := ak.StakingKeeper.BondDenom(ctx)
		if err != nil {
			return err
		}
		ubds, err := ak.StakingKeeper.GetUnbondingDelegations(ctx, hot, 1000)
		if err != nil {
			return fmt.Errorf("v13: read unbonding delegations: %w", err)
		}
		unbonding := math.ZeroInt()
		for _, ubd := range ubds {
			for _, e := range ubd.Entries {
				unbonding = unbonding.Add(e.Balance)
			}
			if err := ak.StakingKeeper.RemoveUnbondingDelegation(ctx, ubd); err != nil {
				return fmt.Errorf("v13: remove unbonding delegation %s: %w", ubd.ValidatorAddress, err)
			}
			logger.Info("v13: removed unbonding delegation", "validator", ubd.ValidatorAddress, "entries", len(ubd.Entries))
		}
		if unbonding.IsPositive() {
			coins := sdk.NewCoins(sdk.NewCoin(bondDenom, unbonding))
			if err := ak.BankKeeper.SendCoinsFromModuleToAccount(ctx, stakingtypes.NotBondedPoolName, recovery, coins); err != nil {
				return fmt.Errorf("v13: pay unbonding balance from not-bonded pool: %w", err)
			}
			expected = expected.Add(coins...)
			logger.Info("v13: paid out unbonding balance", "coins", coins.String(), "delegations", len(ubds))
		}

		// Invariants: sources empty, destination grew by exactly what left them.
		if !ak.BankKeeper.GetAllBalances(ctx, thief).IsZero() {
			return fmt.Errorf("v13: thief address still funded")
		}
		if left, err := ak.StakingKeeper.GetUnbondingDelegations(ctx, hot, 1); err != nil || len(left) != 0 {
			return fmt.Errorf("v13: compromised wallet still has unbonding delegations (err=%v)", err)
		}
		after := ak.BankKeeper.GetAllBalances(ctx, recovery)
		if !after.Equal(before.Add(expected...)) {
			return fmt.Errorf("v13: recovery balance mismatch: before %s + moved %s != after %s", before, expected, after)
		}
		logger.Info("v13: recovery complete", "moved", expected.String(), "recovery_balance", after.String())
		return nil
	}
}
