package app

import (
	"cosmossdk.io/log/v2"
	"encoding/json"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13roles"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Read only a closed disposable follower copy. No keys, network, commit or broadcast.
func TestV131CurrentStateReadiness(t *testing.T) {
	home := os.Getenv("V131_STATE_HOME")
	if home == "" {
		t.Skip("set V131_STATE_HOME to a closed disposable current-state copy")
	}
	db, err := dbm.NewDB("application", dbm.PebbleDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	a := NewChainApp(log.NewNopLogger(), db, nil, true, simtestutil.AppOptionsMap{flags.FlagHome: home}, nil, bam.SetChainID("dungeon-1"))
	require.Greater(t, a.LastBlockHeight(), v13.ForkHeight)
	require.Less(t, a.LastBlockHeight(), int64(22500000))
	ctx := a.NewUncachedContext(false, cmtproto.Header{ChainID: "dungeon-1", Height: 22500000, Time: time.Date(2026, 10, 29, 18, 40, 0, 0, time.UTC)})
	params, err := a.ConsensusParamsKeeper.ParamsStore.Get(ctx)
	require.NoError(t, err)
	ctx = ctx.WithConsensusParams(params)
	keepers := &upgrades.AppKeepers{AccountKeeper: &a.AccountKeeper, WasmKeeper: &a.WasmKeeper, GetStoreKey: a.GetKey}
	addr := func(s string) sdk.AccAddress {
		b, e := a.AccountKeeper.AddressCodec().StringToBytes(s)
		require.NoError(t, e)
		return sdk.AccAddress(b)
	}
	rawBefore := map[string][]byte{}
	configs := map[string]map[string]json.RawMessage{}
	balances := map[string]string{}
	for _, target := range v13roles.Targets {
		account := addr(target.Address)
		rawBefore[target.Address] = a.WasmKeeper.QueryRaw(ctx, account, []byte("config"))
		b, e := a.WasmKeeper.QuerySmart(ctx, account, []byte(`{"config":{}}`))
		require.NoError(t, e)
		var config map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &config))
		configs[target.Address] = config
		balances[target.Address] = a.BankKeeper.GetAllBalances(ctx, account).String()
	}
	broken := append([]v13roles.Target(nil), v13roles.Targets...)
	broken[len(broken)-1].ConfigHash = "incorrect"
	require.Error(t, v13roles.Recover(ctx, keepers, broken))
	for _, target := range v13roles.Targets {
		require.Equal(t, rawBefore[target.Address], a.WasmKeeper.QueryRaw(ctx, addr(target.Address), []byte("config")), "late failure must roll back all writes")
	}
	handler := v13roles.CreateUpgradeHandler(a.ModuleManager, a.configurator, keepers)
	_, err = handler(ctx, upgradetypes.Plan{Name: "wrong-plan", Height: 22500000}, a.ModuleManager.GetVersionMap())
	require.Error(t, err)
	_, err = handler(ctx, upgradetypes.Plan{Name: v13roles.Name, Height: 22500001}, a.ModuleManager.GetVersionMap())
	require.Error(t, err)
	_, err = handler(ctx, upgradetypes.Plan{Name: v13roles.Name, Height: 22500000}, a.ModuleManager.GetVersionMap())
	require.NoError(t, err)
	for _, target := range v13roles.Targets {
		account := addr(target.Address)
		b, e := a.WasmKeeper.QuerySmart(ctx, account, []byte(`{"config":{}}`))
		require.NoError(t, e)
		var config map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &config))
		require.Len(t, config, len(configs[target.Address]))
		for field, before := range configs[target.Address] {
			if field == target.Field {
				var got string
				require.NoError(t, json.Unmarshal(config[field], &got))
				require.Equal(t, v13.RecoveryAddress, got)
			} else {
				require.JSONEq(t, string(before), string(config[field]), target.Address+" "+field)
			}
		}
		info := a.WasmKeeper.GetContractInfo(ctx, account)
		require.Equal(t, target.CodeID, info.CodeID)
		require.Equal(t, target.MigrationAdmin, info.Admin)
		require.Equal(t, balances[target.Address], a.BankKeeper.GetAllBalances(ctx, account).String())
		t.Logf("verified current-state %s code=%d: role recovered; other config, metadata and balance unchanged", target.Address, target.CodeID)
	}
	after := a.WasmKeeper.QueryRaw(ctx, addr(v13roles.Targets[3].Address), []byte("config"))
	require.NoError(t, v13roles.Recover(ctx, keepers, v13roles.Targets))
	require.Equal(t, after, a.WasmKeeper.QueryRaw(ctx, addr(v13roles.Targets[3].Address), []byte("config")))
}
