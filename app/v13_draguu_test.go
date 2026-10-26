package app

import (
	"encoding/json"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
)

// Called by TestV13ForkOnMainnetState after the actual three-block fork. All
// calls run on a disposable DB copy, with no signing key and no Commit.
func assertV13DRAGUURecovery(t *testing.T, a *ChainApp, ctx sdk.Context) {
	t.Helper()
	addr := func(s string) sdk.AccAddress {
		b, err := a.AccountKeeper.AddressCodec().StringToBytes(s)
		require.NoError(t, err)
		return sdk.AccAddress(b)
	}
	query := func(c, msg string, out any) {
		b, err := a.WasmKeeper.QuerySmart(ctx, addr(c), []byte(msg))
		require.NoError(t, err, "query %s %s", c, msg)
		require.NoError(t, json.Unmarshal(b, out))
	}
	var state struct {
		Voting  string `json:"voting_module"`
		Modules []struct {
			Address string `json:"address"`
			Status  string `json:"status"`
		} `json:"proposal_modules"`
	}
	query(v13.DRAGUUCore, `{"dump_state":{}}`, &state)
	require.NotEqual(t, v13.DRAGUUOldVoting, state.Voting)
	require.Len(t, state.Modules, 2)
	var proposal string
	for _, m := range state.Modules {
		if m.Address == v13.DRAGUUOldProposal {
			require.Equal(t, "disabled", m.Status)
		} else {
			require.Equal(t, "enabled", m.Status)
			proposal = m.Address
		}
	}
	require.NotEmpty(t, proposal)
	for _, voter := range []struct{ address, power string }{{v13.RecoveryAddress, "1000000000"}, {v13.CompromisedHotWallet, "0"}} {
		var result struct {
			Power string `json:"power"`
		}
		query(v13.DRAGUUCore, `{"voting_power_at_height":{"address":"`+voter.address+`"}}`, &result)
		require.Equal(t, voter.power, result.Power)
	}
	require.Equal(t, "999999000000000", a.BankKeeper.GetBalance(ctx, addr(v13.DRAGUUCore), v13.DRAGUUDenom).Amount.String())
	require.Equal(t, "1000000000000000", a.BankKeeper.GetSupply(ctx, v13.DRAGUUDenom).Amount.String())
	require.True(t, a.BankKeeper.GetBalance(ctx, addr(v13.DRAGUUOldVoting), v13.DRAGUUDenom).IsZero())
	var config map[string]any
	query(state.Voting, `{"get_config":{}}`, &config)
	require.Equal(t, map[string]any{"unstaking_duration": map[string]any{"time": float64(604800)}}, config)
	query(proposal, `{"config":{}}`, &config)
	require.Equal(t, true, config["allow_revoting"])
	require.Equal(t, true, config["only_members_execute"])
	require.Equal(t, map[string]any{"time": float64(432000)}, config["max_voting_period"])
	require.Equal(t, map[string]any{"time": float64(172800)}, config["min_voting_period"])
	require.Equal(t, map[string]any{"threshold_quorum": map[string]any{"threshold": map[string]any{"percent": "0.67"}, "quorum": map[string]any{"percent": "0.2"}}}, config["threshold"])
	// Re-running the recovery is a verified no-op, with the same module addresses.
	ak := &upgrades.AppKeepers{AccountKeeper: &a.AccountKeeper, BankKeeper: &a.BankKeeper, WasmKeeper: &a.WasmKeeper}
	require.NoError(t, v13.RecoverDRAGUU(ctx, ak))
	var same struct {
		Voting string `json:"voting_module"`
	}
	query(v13.DRAGUUCore, `{"dump_state":{}}`, &same)
	require.Equal(t, state.Voting, same.Voting)
	exec := wasmkeeper.NewDefaultPermissionKeeper(&a.WasmKeeper)
	// Even if the ante freeze were bypassed, the former proposal module cannot
	// invoke a treasury send: core itself rejects disabled module authority.
	malicious, err := json.Marshal(map[string]any{"execute_proposal_hook": map[string]any{"msgs": []any{map[string]any{"bank": map[string]any{"send": map[string]any{"to_address": v13.CompromisedHotWallet, "amount": sdk.NewCoins(sdk.NewInt64Coin(v13.DRAGUUDenom, 1))}}}}}})
	require.NoError(t, err)
	_, err = exec.Execute(ctx, addr(v13.DRAGUUCore), addr(v13.DRAGUUOldProposal), malicious, nil)
	require.ErrorContains(t, err, "disabled")
	require.Equal(t, "999999000000000", a.BankKeeper.GetBalance(ctx, addr(v13.DRAGUUCore), v13.DRAGUUDenom).Amount.String())
	// Clean membership must support the complete proposal -> vote -> execute
	// path. Exercise an actual one-base-unit treasury payment on this copy.
	var policy struct {
		Module struct {
			Address string `json:"addr"`
		} `json:"module"`
	}
	query(proposal, `{"proposal_creation_policy":{}}`, &policy)
	require.NotEmpty(t, policy.Module.Address)
	msg, err := json.Marshal(map[string]any{"propose": map[string]any{"msg": map[string]any{"propose": map[string]any{
		"title": "Recovery verification", "description": "Disposable mainnet-copy test only", "vote": nil,
		"msgs": []any{map[string]any{"bank": map[string]any{"send": map[string]any{"to_address": v13.RecoveryAddress, "amount": sdk.NewCoins(sdk.NewInt64Coin(v13.DRAGUUDenom, 1))}}}},
	}}}})
	require.NoError(t, err)
	_, err = exec.Execute(ctx, addr(policy.Module.Address), addr(v13.CompromisedHotWallet), msg, nil)
	require.Error(t, err, "old key must fail member-only proposal creation even without ante freeze")
	_, err = exec.Execute(ctx, addr(policy.Module.Address), addr(v13.RecoveryAddress), msg, nil)
	require.NoError(t, err, "clean wallet can propose")
	_, err = exec.Execute(ctx, addr(proposal), addr(v13.CompromisedHotWallet), []byte(`{"vote":{"proposal_id":1,"vote":"yes","rationale":null}}`), nil)
	require.Error(t, err, "old key must have no voting power")
	_, err = exec.Execute(ctx, addr(proposal), addr(v13.RecoveryAddress), []byte(`{"vote":{"proposal_id":1,"vote":"yes","rationale":null}}`), nil)
	require.NoError(t, err, "clean wallet can vote")
	later := ctx.WithBlockHeight(ctx.BlockHeight() + 1).WithBlockTime(ctx.BlockTime().Add(5*24*time.Hour + time.Second))
	_, err = exec.Execute(later, addr(proposal), addr(v13.CompromisedHotWallet), []byte(`{"execute":{"proposal_id":1}}`), nil)
	require.Error(t, err, "old key cannot execute new proposals")
	_, err = exec.Execute(later, addr(proposal), addr(v13.RecoveryAddress), []byte(`{"execute":{"proposal_id":1}}`), nil)
	require.NoError(t, err, "clean wallet can execute approved treasury payment")
	require.Equal(t, "999998999999999", a.BankKeeper.GetBalance(later, addr(v13.DRAGUUCore), v13.DRAGUUDenom).Amount.String())
	require.Equal(t, "1", a.BankKeeper.GetBalance(later, addr(v13.RecoveryAddress), v13.DRAGUUDenom).Amount.String())
	t.Logf("DRAGUU recovery verified: old module disabled; old key cannot propose/vote/execute; clean wallet completes treasury governance; voting %s proposal %s pre-propose %s", state.Voting, proposal, policy.Module.Address)
}
