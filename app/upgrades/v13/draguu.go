package v13

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	"cosmossdk.io/math"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

const (
	DRAGUUCore = "dungeon15hww9s7t9en85zxu94xkjtxevhm8nqwpd2pg22cj5tcakj89084qvzds5w"
	DRAGUUOldVoting = "dungeon1q2hu0wkvm47y49x2308xtrwzq48dasxtvcllndpq4pljgawrlrtscuc3rh"
	DRAGUUOldProposal = "dungeon13hl7x24z869zws64ndtgghwh627mshvt6vp4j65wzkur2wx654dq0h8utl"
	DRAGUUDenom = "factory/" + CompromisedHotWallet + "/draguudao"
	DRAGUUStake int64 = 1_000_000_000
	DRAGUUReserve int64 = 999_999_000_000_000
	DRAGUURecoveryItem = "draguu:security_recovery"
)

type draguuModule struct {
	Address string `json:"address"`
	Status string `json:"status"`
}

type draguuState struct {
	Admin string `json:"admin"`
	Voting string `json:"voting_module"`
	Modules []draguuModule `json:"proposal_modules"`
}

// RecoverDRAGUU restores the sole bootstrap stake to the clean wallet. Merely
// unstaking would leave historical proposal permissions behind. The original
// proposal module is therefore disabled, and fresh voting/proposal modules use
// the same audited code and approved settings. No raw contract storage is edited.
// The DAO, treasury, reward contract, token supply and retired mint stay intact.
// This is an atomic, idempotent consensus recovery, never a signed transaction.
func RecoverDRAGUU(ctx context.Context, ak *upgrades.AppKeepers) error {
	if ak.WasmKeeper == nil {
		return fmt.Errorf("v13 DRAGUU: wasm keeper required")
	}
	sdkCtx, write := sdk.UnwrapSDKContext(ctx).CacheContext()
	addr := func(s string) (sdk.AccAddress, error) {
		b, err := ak.AccountKeeper.AddressCodec().StringToBytes(s)
		return sdk.AccAddress(b), err
	}
	core, err := addr(DRAGUUCore)
	if err != nil { return err }
	old, err := addr(CompromisedHotWallet)
	if err != nil { return err }
	recovery, err := addr(RecoveryAddress)
	if err != nil { return err }
	oldVoting, err := addr(DRAGUUOldVoting)
	if err != nil { return err }
	query := func(contract, msg string, out any) error {
		c, err := addr(contract)
		if err != nil { return err }
		b, err := ak.WasmKeeper.QuerySmart(sdkCtx, c, []byte(msg))
		if err != nil { return fmt.Errorf("v13 DRAGUU query %s: %w", contract, err) }
		return json.Unmarshal(b, out)
	}
	equal := func(contract, msg, want string) error {
		var got, expected any
		if err := query(contract, msg, &got); err != nil { return err }
		if err := json.Unmarshal([]byte(want), &expected); err != nil { return err }
		if !reflect.DeepEqual(got, expected) {
			return fmt.Errorf("v13 DRAGUU unexpected state at %s for %s: %v", contract, msg, got)
		}
		return nil
	}
	exec := wasmkeeper.NewDefaultPermissionKeeper(ak.WasmKeeper)
	call := func(contract string, sender sdk.AccAddress, msg any, funds sdk.Coins) error {
		c, err := addr(contract)
		if err != nil { return err }
		b, err := json.Marshal(msg)
		if err != nil { return err }
		if _, err := exec.Execute(sdkCtx, c, sender, b, funds); err != nil {
			return fmt.Errorf("v13 DRAGUU execute %s: %w", contract, err)
		}
		return nil
	}
	jsonMsg := func(s string) json.RawMessage { return json.RawMessage(s) }
	module := func(code uint64, msg any, label string) (map[string]any, error) {
		b, err := json.Marshal(msg)
		return map[string]any{"code_id": code, "msg": b, "admin": map[string]any{"core_module": map[string]any{}}, "funds": []any{}, "label": label}, err
	}
	var state draguuState
	if err := query(DRAGUUCore, `{"dump_state":{}}`, &state); err != nil { return err }
	// Pin the exact already-reviewed code, including the immutable core.
	for _, pin := range []struct{ id uint64; hash string }{
		{113, "93e33dd4f4101635709231e51086f9c7774237f77a610bcb40fc8060b7f4b155"},
		{114, "a3de0ca05344d868d12e5e63103f6b97d3e549bb5a305592d5dcfef71dec7b77"},
		{116, "3dc2b1dc238fa782acc9daa53b0319e1d43caebc5f2a81134de35113265f6b6f"},
		{117, "17e6ffa998b2272dca6c43b16b9d56c4d870f2b5623d01ef3b384ab5ad2b3e86"},
	} {
		info := ak.WasmKeeper.GetCodeInfo(sdkCtx, pin.id)
		if info == nil || hex.EncodeToString(info.CodeHash) != pin.hash { return fmt.Errorf("v13 DRAGUU code %d checksum mismatch", pin.id) }
	}
	info := ak.WasmKeeper.GetContractInfo(sdkCtx, core)
	if info == nil || info.CodeID != 113 || info.Admin != "" || state.Admin != DRAGUUCore {
		return fmt.Errorf("v13 DRAGUU immutable self-admin core mismatch")
	}
	if !ak.BankKeeper.GetBalance(sdkCtx, core, DRAGUUDenom).Amount.Equal(math.NewInt(DRAGUUReserve)) {
		return fmt.Errorf("v13 DRAGUU reserve mismatch")
	}
	if !ak.BankKeeper.GetSupply(sdkCtx, DRAGUUDenom).Amount.Equal(math.NewInt(DRAGUUReserve+DRAGUUStake)) {
		return fmt.Errorf("v13 DRAGUU supply mismatch")
	}
	if state.Voting != DRAGUUOldVoting {
		// A rerun must prove the completed recovery, not silently accept drift.
		if err := equal(DRAGUUCore, `{"get_item":{"key":"`+DRAGUURecoveryItem+`"}}`, `{"item":"v13: bootstrap recovered; legacy proposal module quarantined"}`); err != nil { return err }
		if err := verifyDRAGUURecovered(sdkCtx, ak, state, query); err != nil { return err }
		return nil
	}
	if len(state.Modules) != 1 || state.Modules[0].Address != DRAGUUOldProposal || state.Modules[0].Status != "enabled" {
		return fmt.Errorf("v13 DRAGUU original proposal module mismatch")
	}
	stakers := `{"stakers":[{"address":"`+CompromisedHotWallet+`","balance":"1000000000"}]}`
	for _, check := range []struct{ contract, msg, want string }{
		{DRAGUUOldVoting, `{"list_stakers":{"limit":2}}`, stakers},
		{DRAGUUOldVoting, `{"claims":{"address":"`+CompromisedHotWallet+`"}}`, `{"claims":[]}`},
		{DRAGUUOldVoting, `{"get_hooks":{}}`, `{"hooks":[]}`},
		{DRAGUUOldVoting, `{"get_config":{}}`, `{"unstaking_duration":{"time":604800}}`},
		{DRAGUUOldVoting, `{"active_threshold":{}}`, `{"active_threshold":{"absolute_count":{"count":"100000000"}}}`},
	} {
		if err := equal(check.contract, check.msg, check.want); err != nil { return err }
	}
	before := ak.BankKeeper.GetAllBalances(sdkCtx, core)
	oldBefore := ak.BankKeeper.GetAllBalances(sdkCtx, old)
	recoveryBefore := ak.BankKeeper.GetAllBalances(sdkCtx, recovery)
	// Recover only this verified bootstrap stake, atomically, without using the
	// stolen private key or waiting seven days for a claim to the stolen address.
	if err := call(DRAGUUOldVoting, core, jsonMsg(`{"update_config":{"duration":null}}`), nil); err != nil { return err }
	if err := call(DRAGUUOldVoting, old, jsonMsg(`{"unstake":{"amount":"1000000000"}}`), nil); err != nil { return err }
	if err := call(DRAGUUOldVoting, core, jsonMsg(`{"update_config":{"duration":{"time":604800}}}`), nil); err != nil { return err }
	coins := sdk.NewCoins(sdk.NewInt64Coin(DRAGUUDenom, DRAGUUStake))
	if err := ak.BankKeeper.SendCoins(sdkCtx, old, recovery, coins); err != nil { return err }
	voting, err := module(116, jsonMsg(`{"token_info":{"existing":{"denom":"`+DRAGUUDenom+`"}},"unstaking_duration":{"time":604800},"active_threshold":{"absolute_count":{"count":"100000000"}}}`), "DRAGUU_recovered_voting_v13")
	if err != nil { return err }
	if err := call(DRAGUUCore, core, map[string]any{"update_voting_module": map[string]any{"module": voting}}, nil); err != nil { return err }
	if err := query(DRAGUUCore, `{"dump_state":{}}`, &state); err != nil { return err }
	if err := call(state.Voting, recovery, jsonMsg(`{"stake":{}}`), coins); err != nil { return err }
	pre, err := module(114, jsonMsg(`{"deposit_info":null,"submission_policy":{"specific":{"dao_members":true,"allowlist":[],"denylist":[]}},"extension":{}}`), "DRAGUU_recovered_pre_propose_v13")
	if err != nil { return err }
	proposal := map[string]any{
		"threshold": jsonMsg(`{"threshold_quorum":{"threshold":{"percent":"0.67"},"quorum":{"percent":"0.2"}}}`),
		"max_voting_period": map[string]any{"time":432000}, "min_voting_period": map[string]any{"time":172800},
		"only_members_execute":true, "allow_revoting":true, "close_proposal_on_execution_failure":false,
		"veto":nil, "delegation_module":nil, "pre_propose_info": map[string]any{"module_may_propose":map[string]any{"info":pre}},
	}
	propose, err := module(117, proposal, "DRAGUU_recovered_proposals_v13")
	if err != nil { return err }
	if err := call(DRAGUUCore, core, map[string]any{"update_proposal_modules":map[string]any{"to_add":[]any{propose}, "to_disable":[]string{DRAGUUOldProposal}}}, nil); err != nil { return err }
	if err := call(DRAGUUCore, core, jsonMsg(`{"set_item":{"key":"`+DRAGUURecoveryItem+`","value":"v13: bootstrap recovered; legacy proposal module quarantined"}}`), nil); err != nil { return err }
	if err := query(DRAGUUCore, `{"dump_state":{}}`, &state); err != nil { return err }
	if err := verifyDRAGUURecovered(sdkCtx, ak, state, query); err != nil { return err }
	if !before.Equal(ak.BankKeeper.GetAllBalances(sdkCtx, core)) || !oldBefore.Equal(ak.BankKeeper.GetAllBalances(sdkCtx, old)) || !recoveryBefore.Equal(ak.BankKeeper.GetAllBalances(sdkCtx, recovery)) {
		return fmt.Errorf("v13 DRAGUU unexpected liquid balance change")
	}
	if !ak.BankKeeper.GetBalance(sdkCtx, oldVoting, DRAGUUDenom).IsZero() { return fmt.Errorf("v13 DRAGUU old stake not emptied") }
	write()
	sdk.UnwrapSDKContext(ctx).Logger().Info("v13: DRAGUU recovered", "core", DRAGUUCore, "voting", state.Voting, "recovery", RecoveryAddress)
	return nil
}

func verifyDRAGUURecovered(ctx sdk.Context, ak *upgrades.AppKeepers, s draguuState, query func(string, string, any) error) error {
	if s.Voting == DRAGUUOldVoting || s.Admin != DRAGUUCore || len(s.Modules) != 2 { return fmt.Errorf("v13 DRAGUU recovered modules mismatch") }
	var disabled, enabled int
	for _, m := range s.Modules {
		if m.Address == DRAGUUOldProposal && m.Status == "disabled" { disabled++ }
		if m.Address != DRAGUUOldProposal && m.Status == "enabled" { enabled++ }
	}
	if disabled != 1 || enabled != 1 { return fmt.Errorf("v13 DRAGUU old proposals not quarantined") }
	var stakers struct { Stakers []struct { Address string `json:"address"`; Balance string `json:"balance"` } `json:"stakers"` }
	if err := query(s.Voting, `{"list_stakers":{"limit":2}}`, &stakers); err != nil { return err }
	if len(stakers.Stakers) != 1 || stakers.Stakers[0].Address != RecoveryAddress || stakers.Stakers[0].Balance != "1000000000" {
		return fmt.Errorf("v13 DRAGUU clean bootstrap stake mismatch")
	}
	for _, module := range append([]draguuModule{{Address:s.Voting, Status:"voting"}}, s.Modules...) {
		if module.Status == "disabled" { continue }
		b, err := ak.AccountKeeper.AddressCodec().StringToBytes(module.Address)
		if err != nil { return err }
		ci := ak.WasmKeeper.GetContractInfo(ctx, b)
		code := uint64(117)
		if module.Status == "voting" { code = 116 }
		if ci == nil || ci.CodeID != code || ci.Admin != DRAGUUCore { return fmt.Errorf("v13 DRAGUU recovered code/admin mismatch") }
	}
	return nil
}
