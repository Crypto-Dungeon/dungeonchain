package v13

import (
	"context"
	"encoding/json"
	"fmt"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

// ClosedRoutes are bridge routes v13 removes at the fork. The DGN collateral
// warp to Base (domain 8453) is closed: its validator and relayer keys were
// stolen and the intruder holds Base DGN. Lee re-enrolls it from the new
// admin key once the Base side has new keys.
var ClosedRoutes = []struct {
	Warp   string
	Domain uint32
}{
	{"dungeon1eau6sevxmq5stk2xjwwaemv0t0yr7z8gv438dnkg5lt56d5v4e3q6xs9ut", 8453}, // DGN collateral warp -> Base
}

// CloseBridgeRoutes unenrolls each route as RecoveryAddress (the warp owner)
// and checks list_routes no longer has the domain. Safe to run twice.
func CloseBridgeRoutes(ctx context.Context, ak *upgrades.AppKeepers) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	b, err := ak.AccountKeeper.AddressCodec().StringToBytes(RecoveryAddress)
	if err != nil {
		return err
	}
	owner := sdk.AccAddress(b)
	exec := wasmkeeper.NewDefaultPermissionKeeper(ak.WasmKeeper)

	open := func(c sdk.AccAddress, domain uint32) (bool, error) {
		res, err := ak.WasmKeeper.QuerySmart(sdkCtx, c, []byte(`{"router":{"list_routes":{}}}`))
		if err != nil {
			return false, err
		}
		var r struct {
			Routes []struct {
				Domain uint32  `json:"domain"`
				Route  *string `json:"route"`
			} `json:"routes"`
		}
		if err := json.Unmarshal(res, &r); err != nil {
			return false, err
		}
		for _, x := range r.Routes {
			if x.Domain == domain && x.Route != nil {
				return true, nil
			}
		}
		return false, nil
	}

	for _, cr := range ClosedRoutes {
		cb, err := ak.AccountKeeper.AddressCodec().StringToBytes(cr.Warp)
		if err != nil {
			return fmt.Errorf("v13: warp %s: %w", cr.Warp, err)
		}
		c := sdk.AccAddress(cb)
		if isOpen, err := open(c, cr.Domain); err != nil {
			return fmt.Errorf("v13: list routes of %s: %w", cr.Warp, err)
		} else if !isOpen {
			continue
		}
		msg := fmt.Sprintf(`{"router":{"set_route":{"set":{"domain":%d,"route":null}}}}`, cr.Domain)
		if _, err := exec.Execute(sdkCtx, c, owner, []byte(msg), nil); err != nil {
			return fmt.Errorf("v13: close route %d on %s: %w", cr.Domain, cr.Warp, err)
		}
		if isOpen, err := open(c, cr.Domain); err != nil || isOpen {
			return fmt.Errorf("v13: route %d on %s still open after close (err=%v)", cr.Domain, cr.Warp, err)
		}
		sdkCtx.Logger().Info("v13: bridge route closed", "warp", cr.Warp, "domain", cr.Domain)
	}
	return nil
}
