package app

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/stretchr/testify/require"

	"github.com/Crypto-Dungeon/dungeonchain/app/decorators"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
)

// The operator accounts must be exactly the account form of the rotated
// validators, and every frozen entry must decode.
func TestV13FrozenListMatchesValidators(t *testing.T) {
	app := Setup(t)
	require.Len(t, v13.OperatorAccounts, len(v13.Rotations))
	for i, r := range v13.Rotations {
		val, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(r.Validator)
		require.NoError(t, err)
		acc, err := app.AccountKeeper.AddressCodec().StringToBytes(v13.OperatorAccounts[i])
		require.NoError(t, err)
		require.Equal(t, val, acc, "%s vs %s", r.Validator, v13.OperatorAccounts[i])
	}
	require.Len(t, mustDecodeAccounts(app.AccountKeeper.AddressCodec(), v13.FrozenAccounts()), 5)
}

func TestFrozenSignerDecorator(t *testing.T) {
	app := Setup(t)
	frozen := sdk.AccAddress([]byte("frozen-account------"))
	other := sdk.AccAddress([]byte("other-account-------"))
	d := decorators.NewFrozenSignerDecorator(app.AppCodec(), [][]byte{frozen}, 100)
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
	coins := sdk.NewCoins(sdk.NewInt64Coin("udgn", 1))

	build := func(granter sdk.AccAddress, msgs ...sdk.Msg) sdk.Tx {
		b := app.TxConfig().NewTxBuilder()
		require.NoError(t, b.SetMsgs(msgs...))
		if granter != nil {
			b.SetFeeGranter(granter)
		}
		return b.GetTx()
	}
	run := func(height int64, tx sdk.Tx) error {
		ctx := app.NewContextLegacy(false, cmtproto.Header{Height: height, Time: time.Now().UTC()})
		_, err := d.AnteHandle(ctx, tx, false, next)
		return err
	}

	fromFrozen := build(nil, banktypes.NewMsgSend(frozen, other, coins))
	require.NoError(t, run(99, fromFrozen), "not frozen before the fork height")
	require.ErrorContains(t, run(100, fromFrozen), "frozen")

	require.NoError(t, run(100, build(nil, banktypes.NewMsgSend(other, frozen, coins))), "sending TO a frozen account is fine")
	require.ErrorContains(t, run(100, build(frozen, banktypes.NewMsgSend(other, other, coins))), "frozen", "fee granter")

	exec := authz.NewMsgExec(other, []sdk.Msg{banktypes.NewMsgSend(frozen, other, coins)})
	require.ErrorContains(t, run(100, build(nil, &exec)), "frozen", "authz on behalf of a frozen granter")
	nested := authz.NewMsgExec(other, []sdk.Msg{&exec})
	require.ErrorContains(t, run(100, build(nil, &nested)), "frozen", "nested authz")
}

// SweepOperators moves commission, self-delegation rewards and liquid coins
// of the operator account to the recovery address and leaves it empty.
func TestV13SweepOperators(t *testing.T) {
	app := Setup(t)
	ctx := app.NewContextLegacy(false, cmtproto.Header{Height: 2, Time: time.Now().UTC()})
	vals, err := app.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	valBz, err := app.StakingKeeper.ValidatorAddressCodec().StringToBytes(vals[0].OperatorAddress)
	require.NoError(t, err)
	op := sdk.AccAddress(valBz)
	denom, err := app.StakingKeeper.BondDenom(ctx)
	require.NoError(t, err)

	liquid := sdk.NewCoins(sdk.NewInt64Coin(denom, 7_000_000), sdk.NewInt64Coin("ibc/ABCD", 3))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, liquid))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, op, liquid))

	// accrue commission + rewards: fund the distribution module and allocate
	reward := sdk.NewCoins(sdk.NewInt64Coin(denom, 1_000_000))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, reward))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, "distribution", reward))
	// The test validator has a 0% rate, so book the commission directly
	// (backed by the distribution module and the outstanding-rewards pool).
	dec := sdk.NewDecCoinsFromCoins(reward...)
	require.NoError(t, app.DistrKeeper.SetValidatorAccumulatedCommission(ctx, sdk.ValAddress(valBz), distrtypes.ValidatorAccumulatedCommission{Commission: dec}))
	require.NoError(t, app.DistrKeeper.SetValidatorOutstandingRewards(ctx, sdk.ValAddress(valBz), distrtypes.ValidatorOutstandingRewards{Rewards: dec}))

	b, err := app.AccountKeeper.AddressCodec().StringToBytes(v13.RecoveryAddress)
	require.NoError(t, err)
	recovery := sdk.AccAddress(b)
	before := app.BankKeeper.GetBalance(ctx, recovery, denom).Amount

	ak := &upgrades.AppKeepers{AccountKeeper: &app.AccountKeeper, BankKeeper: &app.BankKeeper, StakingKeeper: app.StakingKeeper, DistrKeeper: &app.DistrKeeper}
	rot := []v13.KeyRotation{{Validator: vals[0].OperatorAddress}}
	require.NoError(t, v13.SweepOperators(ctx, ak, rot))

	require.True(t, app.BankKeeper.GetAllBalances(ctx, op).IsZero(), "operator account emptied")
	gained := app.BankKeeper.GetBalance(ctx, recovery, denom).Amount.Sub(before)
	require.True(t, gained.Equal(math.NewInt(8_000_000)), "recovery gets liquid 7 + commission 1 DGN, got %s", gained)
	require.True(t, app.BankKeeper.GetBalance(ctx, recovery, "ibc/ABCD").Amount.Equal(math.NewInt(3)))
	comm, err := app.DistrKeeper.GetValidatorAccumulatedCommission(ctx, sdk.ValAddress(valBz))
	require.NoError(t, err)
	require.True(t, comm.Commission.IsZero(), "commission withdrawn")

	require.NoError(t, v13.SweepOperators(ctx, ak, rot), "second run is a no-op")
}
