package app

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/require"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
)

// The node sets the dungeon bech32 prefixes in cmd/dungeond/root.go; the test
// binary never runs that, so the app's address codec would otherwise be built
// for "cosmos" and reject the real dungeon1… addresses in v13/constants.go.
func init() {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
}

type v13Fixture struct{}

func (v13Fixture) GetVersionMap() module.VersionMap { return module.VersionMap{"bank": 1} }
func (f v13Fixture) RunMigrations(context.Context, module.Configurator, module.VersionMap) (module.VersionMap, error) {
	return f.GetVersionMap(), nil
}

// TestV13Recovery reproduces the post-theft state: the thief holds the stolen
// DGN plus a second denom, the compromised wallet has two unbonding entries on
// two validators and a little dust, the recovery address has a small balance.
// After the handler: thief and hot wallet empty, no unbonding left, recovery
// holds everything, not-bonded pool shrank by the unbonding total.
func TestV13Recovery(t *testing.T) {
	app := Setup(t)
	ctx := app.NewContextLegacy(false, cmtproto.Header{Height: 2, Time: time.Now().UTC()})
	denom, err := app.StakingKeeper.BondDenom(ctx)
	require.NoError(t, err)

	addr := func(bech string) sdk.AccAddress {
		b, err := app.AccountKeeper.AddressCodec().StringToBytes(bech)
		require.NoError(t, err)
		return sdk.AccAddress(b)
	}
	thief := addr(v13.ThiefAddress)
	hot := addr(v13.CompromisedHotWallet)
	recovery := addr(v13.RecoveryAddress)

	fund := func(to sdk.AccAddress, coins sdk.Coins) {
		require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, to, coins))
	}
	stolen := sdk.NewCoins(sdk.NewInt64Coin(denom, 31_949_067_859_539), sdk.NewInt64Coin("ibc/ABCD", 7))
	dust := sdk.NewCoins(sdk.NewInt64Coin(denom, 43_404_778))
	seed := sdk.NewCoins(sdk.NewInt64Coin(denom, 2_004_176_338))
	fund(thief, stolen)
	fund(hot, dust)
	fund(recovery, seed)

	// Two unbonding delegations, three entries, backed by the not-bonded pool.
	val1 := sdk.ValAddress([]byte("validator-one-------"))
	val2 := sdk.ValAddress([]byte("validator-two-------"))
	mature := ctx.BlockTime().Add(20 * 24 * time.Hour)
	ubd1 := stakingtypes.NewUnbondingDelegation(hot, val1, 22314372, mature, math.NewInt(4_652_042_000_000), 1, app.StakingKeeper.ValidatorAddressCodec(), app.AccountKeeper.AddressCodec())
	ubd1.Entries = append(ubd1.Entries, stakingtypes.NewUnbondingDelegationEntry(22315111, mature.Add(time.Hour), math.NewInt(100_000_000_000), 2))
	ubd2 := stakingtypes.NewUnbondingDelegation(hot, val2, 22314373, mature, math.NewInt(1_300_000_000_000), 3, app.StakingKeeper.ValidatorAddressCodec(), app.AccountKeeper.AddressCodec())
	require.NoError(t, app.StakingKeeper.SetUnbondingDelegation(ctx, ubd1))
	require.NoError(t, app.StakingKeeper.SetUnbondingDelegation(ctx, ubd2))
	unbondingTotal := math.NewInt(4_652_042_000_000 + 100_000_000_000 + 1_300_000_000_000)
	poolCoins := sdk.NewCoins(sdk.NewCoin(denom, unbondingTotal))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, poolCoins))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, stakingtypes.NotBondedPoolName, poolCoins))
	poolBefore := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName), denom)

	handler := v13.CreateUpgradeHandler(v13Fixture{}, app.configurator, &upgrades.AppKeepers{
		AccountKeeper: &app.AccountKeeper, BankKeeper: &app.BankKeeper, StakingKeeper: app.StakingKeeper,
	})

	// Wrong plan name must refuse before touching anything.
	_, err = handler(ctx, upgradetypes.Plan{Name: "v12", Height: 2}, module.VersionMap{})
	require.Error(t, err)
	require.Equal(t, stolen, app.BankKeeper.GetAllBalances(ctx, thief))

	vm, err := handler(ctx, upgradetypes.Plan{Name: v13.UpgradeName, Height: 2}, module.VersionMap{})
	require.NoError(t, err)
	require.Equal(t, uint64(1), vm["bank"])

	require.True(t, app.BankKeeper.GetAllBalances(ctx, thief).IsZero(), "thief still funded")
	require.True(t, app.BankKeeper.GetAllBalances(ctx, hot).IsZero(), "hot wallet still funded")
	left, err := app.StakingKeeper.GetUnbondingDelegations(ctx, hot, 10)
	require.NoError(t, err)
	require.Empty(t, left)

	want := seed.Add(stolen...).Add(dust...).Add(sdk.NewCoin(denom, unbondingTotal))
	require.Equal(t, want, app.BankKeeper.GetAllBalances(ctx, recovery))
	poolAfter := app.BankKeeper.GetBalance(ctx, app.AccountKeeper.GetModuleAddress(stakingtypes.NotBondedPoolName), denom)
	require.True(t, poolBefore.Amount.Sub(unbondingTotal).Equal(poolAfter.Amount), "not-bonded pool: before %s after %s", poolBefore.Amount, poolAfter.Amount)

	// Running again on the already-recovered state is a no-op, not an error.
	_, err = handler(ctx, upgradetypes.Plan{Name: v13.UpgradeName, Height: 3}, module.VersionMap{})
	require.NoError(t, err)
	require.Equal(t, want, app.BankKeeper.GetAllBalances(ctx, recovery))

	// The governance-free fork entry point runs the same recovery: fund the
	// thief again and let the fork move it.
	fund(thief, stolen)
	require.NotPanics(t, func() {
		v13.RunForkLogic(ctx, &upgrades.AppKeepers{AccountKeeper: &app.AccountKeeper, BankKeeper: &app.BankKeeper, StakingKeeper: app.StakingKeeper})
	})
	require.True(t, app.BankKeeper.GetAllBalances(ctx, thief).IsZero())
	require.Equal(t, want.Add(stolen...), app.BankKeeper.GetAllBalances(ctx, recovery))
	require.Equal(t, int64(0), v13.ForkHeight, "ForkHeight must stay 0 in the repo; set it only in the halt build")
}
