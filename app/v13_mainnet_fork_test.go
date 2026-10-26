package app

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
)

// TestV13ForkOnMainnetState runs the v13 fork block and the two after it on a
// COPY of a real node home (V13_STATE_HOME, pebbledb, halted at ForkHeight-1).
// It never commits; the copy is disposable. Skipped unless the env var is set.
func TestV13ForkOnMainnetState(t *testing.T) {
	home := os.Getenv("V13_STATE_HOME")
	if home == "" {
		t.Skip("set V13_STATE_HOME to a copied node home")
	}
	db, err := dbm.NewDB("application", dbm.PebbleDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	a := NewChainApp(log.NewNopLogger(), db, nil, true, simtestutil.AppOptionsMap{flags.FlagHome: home}, nil, bam.SetChainID("dungeon-1"))
	require.Equal(t, v13.ForkHeight-1, a.LastBlockHeight(), "state copy must stop right before the fork")

	start := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	ctx := a.NewUncachedContext(false, cmtproto.Header{ChainID: "dungeon-1", Height: v13.ForkHeight, Time: start})
	cp, err := a.ConsensusParamsKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	ctx = ctx.WithConsensusParams(cp)

	addr := func(bech string) sdk.AccAddress {
		b, err := a.AccountKeeper.AddressCodec().StringToBytes(bech)
		require.NoError(t, err)
		return sdk.AccAddress(b)
	}
	thief, hot, recovery := addr(v13.ThiefAddress), addr(v13.CompromisedHotWallet), addr(v13.RecoveryAddress)
	denom, err := a.StakingKeeper.BondDenom(ctx)
	require.NoError(t, err)

	thiefBefore := a.BankKeeper.GetBalance(ctx, thief, denom).Amount
	ubds, err := a.StakingKeeper.GetUnbondingDelegations(ctx, hot, 1000)
	require.NoError(t, err)
	unbonding := math.ZeroInt()
	for _, u := range ubds {
		for _, e := range u.Entries {
			unbonding = unbonding.Add(e.Balance)
		}
	}
	recBefore := a.BankKeeper.GetBalance(ctx, recovery, denom).Amount
	opHeld := math.ZeroInt() // liquid DGN of the operator accounts; commission comes on top
	for _, o := range v13.OperatorAccounts {
		opHeld = opHeld.Add(a.BankKeeper.GetBalance(ctx, addr(o), denom).Amount)
	}
	t.Logf("before: thief %s udgn, hot unbonding %s udgn in %d delegations, recovery %s udgn", thiefBefore, unbonding, len(ubds), recBefore)
	require.True(t, thiefBefore.IsPositive(), "thief should still hold the stolen DGN")

	oldKeys := map[string][]byte{}
	newKeys := map[string][]byte{}
	for _, r := range v13.Rotations {
		vb, err := a.StakingKeeper.ValidatorAddressCodec().StringToBytes(r.Validator)
		require.NoError(t, err)
		v, err := a.StakingKeeper.GetValidator(ctx, vb)
		require.NoError(t, err, r.Validator)
		require.False(t, v.IsJailed(), "%s jailed", r.Validator)
		oldKeys[r.Validator] = v.ConsensusPubkey.GetCachedValue().(cryptotypes.PubKey).Bytes()
		newKeys[r.Validator], err = base64.StdEncoding.DecodeString(r.NewPubKey)
		require.NoError(t, err)
	}

	type upd struct {
		key   []byte
		power int64
	}
	var updates []upd
	for h := v13.ForkHeight; h < v13.ForkHeight+3; h++ {
		bctx := ctx.WithBlockHeight(h).WithBlockTime(start.Add(time.Duration(h-v13.ForkHeight) * 6 * time.Second))
		_, err := a.PreBlocker(bctx, nil)
		require.NoError(t, err, "preblock %d", h)
		_, err = a.BeginBlocker(bctx)
		require.NoError(t, err, "beginblock %d", h)
		eb, err := a.EndBlocker(bctx)
		require.NoError(t, err, "endblock %d", h)
		for _, u := range eb.ValidatorUpdates {
			updates = append(updates, upd{u.PubKey.GetEd25519(), u.Power})
		}
		t.Logf("height %d: %d validator updates", h, len(eb.ValidatorUpdates))
		if h == v13.ForkHeight {
			require.True(t, a.BankKeeper.GetAllBalances(bctx, thief).IsZero(), "thief emptied")
			left, err := a.StakingKeeper.GetUnbondingDelegations(bctx, hot, 1)
			require.NoError(t, err)
			require.Empty(t, left)
			fees := math.NewInt(3_000_000)
			got := a.BankKeeper.GetBalance(bctx, recovery, denom).Amount.Sub(recBefore)
			// thief + unbonding + hot dust, minus the three rotation fees
			require.True(t, got.GTE(thiefBefore.Add(unbonding).Add(opHeld).Sub(fees)), "recovery got %s", got)
			for _, cr := range v13.ClosedRoutes {
				res, err := a.WasmKeeper.QuerySmart(bctx, addr(cr.Warp), []byte(`{"router":{"list_routes":{}}}`))
				require.NoError(t, err)
				require.NotContains(t, string(res), fmt.Sprintf(`"domain":%d,"route":"`, cr.Domain), "route still open: %s", res)
				t.Logf("routes after close: %s", res)
			}
			for _, rt := range v13.RoleTransfers {
				res, err := a.WasmKeeper.QuerySmart(bctx, addr(rt.Contract), []byte(rt.Check))
				require.NoError(t, err)
				for _, f := range rt.Fields {
					require.Contains(t, string(res), `"`+f+`":"`+v13.RecoveryAddress+`"`, rt.Contract)
				}
			}
			for _, o := range v13.OperatorAccounts {
				require.True(t, a.BankKeeper.GetAllBalances(bctx, addr(o)).IsZero(), "operator %s emptied", o)
			}
			for _, f := range v13.FrozenAccounts() {
				b := a.TxConfig().NewTxBuilder()
				require.NoError(t, b.SetMsgs(banktypes.NewMsgSend(addr(f), recovery, sdk.NewCoins(sdk.NewInt64Coin(denom, 1)))))
				b.SetGasLimit(200_000)
				_, err := a.AnteHandler()(bctx, b.GetTx(), false)
				require.ErrorContains(t, err, "frozen", f)
			}
			t.Logf("recovery gained %s udgn", got)
		}
	}

	has := func(key []byte, positive bool) bool {
		for _, u := range updates {
			if string(u.key) == string(key) && (u.power > 0) == positive {
				return true
			}
		}
		return false
	}
	for _, r := range v13.Rotations {
		require.True(t, has(newKeys[r.Validator], true), "%s: new key never reached CometBFT with power", r.Validator)
		require.True(t, has(oldKeys[r.Validator], false), "%s: old key never removed", r.Validator)
		vb, _ := a.StakingKeeper.ValidatorAddressCodec().StringToBytes(r.Validator)
		v, err := a.StakingKeeper.GetValidator(ctx, vb)
		require.NoError(t, err)
		require.Equal(t, newKeys[r.Validator], v.ConsensusPubkey.GetCachedValue().(cryptotypes.PubKey).Bytes(), "%s stored key", r.Validator)
		require.False(t, v.IsJailed())
	}
	assertV13DRAGUURecovery(t, a, ctx.WithBlockHeight(v13.ForkHeight+3).WithBlockTime(start.Add(18*time.Second)))
}
