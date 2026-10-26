// Package v13roles is a proposed, separately coordinated follow-up to v13.0.1.
// Do not add this binary to an existing validator rollout without agreement.
package v13roles

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const Name = "v13-contract-roles"

// Proposed first height after the halted v13.0.1 block. Requires explicit
// validator coordination before release or deployment; not a live migration.
const ForkHeight int64 = 22368770

var Fork = upgrades.Fork{UpgradeName: Name, UpgradeHeight: ForkHeight, BeginForkLogic: RunFork}

func RunFork(ctx sdk.Context, keepers *upgrades.AppKeepers) {
	if ctx.ChainID() != "dungeon-1" {
		return
	}
	if err := Recover(ctx, keepers, Targets); err != nil {
		panic(fmt.Errorf("%s: %w", Name, err))
	}
}

// replaceRole preserves every source byte other than one role string. For a
// rerun, replacing the new role back with the old role must reproduce the exact
// pinned source hash. A changed unrelated config field is always rejected.
func replaceRole(raw []byte, field, oldValue, newValue, expectedHash string) ([]byte, error) {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config JSON: %w", err)
	}
	var value string
	if err := json.Unmarshal(cfg[field], &value); err != nil {
		return nil, fmt.Errorf("missing/string role %s required", field)
	}
	if value != oldValue && value != newValue {
		return nil, fmt.Errorf("unexpected %s value", field)
	}
	oldJSON, _ := json.Marshal(oldValue)
	newJSON, _ := json.Marshal(newValue)
	oldPair := append([]byte(fmt.Sprintf("%q:", field)), oldJSON...)
	newPair := append([]byte(fmt.Sprintf("%q:", field)), newJSON...)
	normalized := raw
	if value == newValue {
		if bytes.Count(raw, newPair) != 1 {
			return nil, fmt.Errorf("ambiguous %s encoding", field)
		}
		normalized = bytes.Replace(raw, newPair, oldPair, 1)
	}
	sum := sha256.Sum256(normalized)
	if hex.EncodeToString(sum[:]) != expectedHash {
		return nil, fmt.Errorf("config differs from pinned state")
	}
	if bytes.Count(normalized, oldPair) != 1 {
		return nil, fmt.Errorf("ambiguous %s encoding", field)
	}
	return bytes.Replace(normalized, oldPair, newPair, 1), nil
}

// Recover changes exactly five role strings inside a cached context. It does
// not execute any contract message, alter code/admin metadata, unfreeze the
// old signer, transfer coins, or change a pool toggle. Failure writes nothing.
// This explicit state correction is necessary for immutable Fantasy Sports,
// which has neither an admin setter nor a Wasm migration administrator.
func Recover(ctx context.Context, ak *upgrades.AppKeepers, targets []Target) error {
	if ak.WasmKeeper == nil || ak.AccountKeeper == nil || ak.GetStoreKey == nil {
		return fmt.Errorf("required keepers unavailable")
	}
	cached, write := sdk.UnwrapSDKContext(ctx).CacheContext()
	key := ak.GetStoreKey(wasmtypes.StoreKey)
	if key == nil {
		return fmt.Errorf("wasm store key unavailable")
	}
	oldBytes, err := ak.AccountKeeper.AddressCodec().StringToBytes(v13.CompromisedHotWallet)
	if err != nil {
		return err
	}
	newBytes, err := ak.AccountKeeper.AddressCodec().StringToBytes(v13.RecoveryAddress)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if seen[target.Address] {
			return fmt.Errorf("duplicate recovery target %s", target.Address)
		}
		seen[target.Address] = true
		contractBytes, err := ak.AccountKeeper.AddressCodec().StringToBytes(target.Address)
		if err != nil {
			return err
		}
		contract := sdk.AccAddress(contractBytes)
		info := ak.WasmKeeper.GetContractInfo(cached, contract)
		if info == nil || info.CodeID != target.CodeID || info.Admin != target.MigrationAdmin {
			return fmt.Errorf("%s contract code/admin mismatch", target.Address)
		}
		code := ak.WasmKeeper.GetCodeInfo(cached, target.CodeID)
		if code == nil || hex.EncodeToString(code.CodeHash) != target.CodeHash {
			return fmt.Errorf("%s code hash mismatch", target.Address)
		}
		oldValue, newValue := v13.CompromisedHotWallet, v13.RecoveryAddress
		if target.Canonical {
			oldValue = base64.StdEncoding.EncodeToString(oldBytes)
			newValue = base64.StdEncoding.EncodeToString(newBytes)
		}
		raw := ak.WasmKeeper.QueryRaw(cached, contract, []byte("config"))
		after, err := replaceRole(raw, target.Field, oldValue, newValue, target.ConfigHash)
		if err != nil {
			return fmt.Errorf("%s: %w", target.Address, err)
		}
		storeKey := append(wasmtypes.GetContractStorePrefix(contract), []byte("config")...)
		cached.KVStore(key).Set(storeKey, after)
		got, err := ak.WasmKeeper.QuerySmart(cached, contract, []byte(`{"config":{}}`))
		if err != nil {
			return fmt.Errorf("%s config query after recovery: %w", target.Address, err)
		}
		var config map[string]json.RawMessage
		if err := json.Unmarshal(got, &config); err != nil {
			return err
		}
		var role string
		if err := json.Unmarshal(config[target.Field], &role); err != nil || role != v13.RecoveryAddress {
			return fmt.Errorf("%s smart query did not confirm recovered role", target.Address)
		}
	}
	write()
	sdk.UnwrapSDKContext(ctx).Logger().Info("remaining contract roles recovered", "upgrade", Name, "contracts", len(targets))
	return nil
}
