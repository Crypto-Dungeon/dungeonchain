package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13roles"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// This test opens only an explicitly supplied disposable, closed database copy.
// It never commits, broadcasts, starts a node, or reads any signer material.
func TestV13RolesOnMainnetState(t *testing.T) {
	home := os.Getenv("V13_STATE_HOME")
	if home == "" {
		t.Skip("set V13_STATE_HOME to a disposable pre-v13 state copy")
	}
	db, err := dbm.NewDB("application", dbm.PebbleDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	a := NewChainApp(log.NewNopLogger(), db, nil, true, simtestutil.AppOptionsMap{flags.FlagHome: home}, nil, bam.SetChainID("dungeon-1"))
	require.Equal(t, v13.ForkHeight-1, a.LastBlockHeight())
	ctx := a.NewUncachedContext(false, cmtproto.Header{ChainID: "dungeon-1", Height: v13.ForkHeight, Time: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)})
	params, err := a.ConsensusParamsKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	ctx = ctx.WithConsensusParams(params)
	_, err = a.PreBlocker(ctx, nil)
	require.NoError(t, err)
	_, err = a.BeginBlocker(ctx)
	require.NoError(t, err)
	_, err = a.EndBlocker(ctx)
	require.NoError(t, err)
	ctx = ctx.WithBlockHeight(v13roles.ForkHeight).WithBlockTime(ctx.BlockTime().Add(6 * time.Second))
	keepers := &upgrades.AppKeepers{AccountKeeper: &a.AccountKeeper, WasmKeeper: &a.WasmKeeper, GetStoreKey: a.GetKey}
	addr := func(s string) sdk.AccAddress {
		b, err := a.AccountKeeper.AddressCodec().StringToBytes(s)
		require.NoError(t, err)
		return sdk.AccAddress(b)
	}
	first := addr(v13roles.Targets[0].Address)
	firstBefore := a.WasmKeeper.QueryRaw(ctx, first, []byte("config"))
	broken := append([]v13roles.Target(nil), v13roles.Targets...)
	broken[len(broken)-1].ConfigHash = "incorrect"
	require.Error(t, v13roles.Recover(ctx, keepers, broken))
	require.Equal(t, firstBefore, a.WasmKeeper.QueryRaw(ctx, first, []byte("config")), "last-target failure must roll back earlier writes")
	mismatched := append([]v13roles.Target(nil), v13roles.Targets...)
	mismatched[0].MigrationAdmin = "unexpected"
	require.ErrorContains(t, v13roles.Recover(ctx, keepers, mismatched), "code/admin mismatch")
	configs := map[string]map[string]json.RawMessage{}
	balances := map[string]string{}
	for _, target := range v13roles.Targets {
		b, err := a.WasmKeeper.QuerySmart(ctx, addr(target.Address), []byte(`{"config":{}}`))
		require.NoError(t, err)
		var config map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &config))
		configs[target.Address] = config
		balances[target.Address] = a.BankKeeper.GetAllBalances(ctx, addr(target.Address)).String()
	}
	require.NoError(t, v13roles.Recover(ctx, keepers, v13roles.Targets))
	for _, target := range v13roles.Targets {
		b, err := a.WasmKeeper.QuerySmart(ctx, addr(target.Address), []byte(`{"config":{}}`))
		require.NoError(t, err)
		var config map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &config))
		require.Len(t, config, len(configs[target.Address]))
		for field, before := range configs[target.Address] {
			if field == target.Field {
				var role string
				require.NoError(t, json.Unmarshal(config[field], &role))
				require.Equal(t, v13.RecoveryAddress, role)
			} else {
				require.JSONEq(t, string(before), string(config[field]), target.Address+" "+field)
			}
		}
		info := a.WasmKeeper.GetContractInfo(ctx, addr(target.Address))
		require.Equal(t, target.CodeID, info.CodeID)
		require.Equal(t, target.MigrationAdmin, info.Admin)
		require.Equal(t, balances[target.Address], a.BankKeeper.GetAllBalances(ctx, addr(target.Address)).String())
		t.Logf("verified %s: %s moved; all other config fields, code, migration admin and balance unchanged", target.Address, target.Field)
	}
	raw := a.WasmKeeper.QueryRaw(ctx, first, []byte("config"))
	require.NoError(t, v13roles.Recover(ctx, keepers, v13roles.Targets))
	require.Equal(t, raw, a.WasmKeeper.QueryRaw(ctx, first, []byte("config")), "rerun is a no-op")
}
