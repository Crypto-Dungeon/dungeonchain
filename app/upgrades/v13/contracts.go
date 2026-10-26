package v13

import (
	"context"
	"encoding/json"
	"fmt"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

// RoleTransfer hands one contract role held by the frozen hot wallet to
// RecoveryAddress. The old key can never sign again (it is frozen), so v13
// executes as it. Check is a smart query whose Fields must all read
// RecoveryAddress afterwards.
type RoleTransfer struct {
	Contract string
	Calls    []RoleCall // run in order
	Check    string     // smart query
	Fields   []string
}

type RoleCall struct {
	AsOld bool // caller = CompromisedHotWallet, else RecoveryAddress
	Msg   string
}

const (
	cw721Transfer = `{"base":{"update_ownership":{"transfer_ownership":{"new_owner":"` + RecoveryAddress + `","expiry":null}}}}`
	cw721Accept   = `{"base":{"update_ownership":"accept_ownership"}}`
	clobAdmin     = `{"update_config":{"admin":"` + RecoveryAddress + `"}}`
	clobAccept    = `{"accept_admin":{}}`
	clobAdminFees = `{"update_config":{"admin":"` + RecoveryAddress + `","fee_collector":"` + RecoveryAddress + `"}}`
)

func cw721Role(addr string) RoleTransfer {
	return RoleTransfer{addr, []RoleCall{{true, cw721Transfer}, {false, cw721Accept}}, `{"ownership":{}}`, []string{"owner"}}
}

func clobRole(addr, msg string, fields ...string) RoleTransfer {
	// deployed code 196 is two-step: update_config sets pending_admin
	return RoleTransfer{addr, []RoleCall{{true, msg}, {false, clobAccept}}, `{"config":{}}`, fields}
}

// RoleTransfers: the cw721 (code 105) collections whose owner is the minter,
// and the three CLOBs whose admin sets fees. The other old-key configs
// (Fantasy, Kindred, auto-swap, castle-v3) have no setter and hold little.
var RoleTransfers = []RoleTransfer{
	cw721Role("dungeon13000dx70vt59u9k7hlxc9nsv6z3yeuhq3c5fkr2rpn0lchztjxqqttsaat"),                                         // heroes-v2
	cw721Role("dungeon15ggwj9un6qrmu4nj5ev6l7kpdcr00td03ff2mmj4cyhl8u8vjd2qhdzy63"),                                         // sky-mercenaries carts
	cw721Role("dungeon1z6cxl4a9nxjdehymwqktdjulzmdhmyq228h4qm2rq7hepavqwszqh90cn3"),                                         // Account NFT
	clobRole("dungeon1llgutmsyf4xsgkraenenh7xhmcr8uzpu4ym9ghlsrvtxr59tggtskqtkz8", clobAdmin, "admin"),                      // ATOM-USDC CLOB
	clobRole("dungeon1efnsf3ruvvq5gtywy02nc05w0ecz8la2m0dzgyd4xemrawmk756qmk4vm4", clobAdmin, "admin"),                      // DGN-USDC CLOB
	clobRole("dungeon1kaz0vatk4c8ev0xpa0wsctj3y3q4acstujw37jhgwr6n7mhm2dcsv9quhw", clobAdminFees, "admin", "fee_collector"), // rehearsal CLOB, fees also went to the old wallet
}

// TransferContractRoles runs RoleTransfers and verifies each with its query.
// A contract whose role already reads RecoveryAddress is skipped, so it is
// safe to run twice.
func TransferContractRoles(ctx context.Context, ak *upgrades.AppKeepers, transfers []RoleTransfer) error {
	if len(transfers) == 0 {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	logger := sdkCtx.Logger().With("upgrade", UpgradeName)
	addr := func(bech string) (sdk.AccAddress, error) {
		b, err := ak.AccountKeeper.AddressCodec().StringToBytes(bech)
		return sdk.AccAddress(b), err
	}
	old, err := addr(CompromisedHotWallet)
	if err != nil {
		return err
	}
	recovery, err := addr(RecoveryAddress)
	if err != nil {
		return err
	}
	exec := wasmkeeper.NewDefaultPermissionKeeper(ak.WasmKeeper)

	var last []byte
	done := func(c sdk.AccAddress, t RoleTransfer) (bool, error) {
		res, err := ak.WasmKeeper.QuerySmart(sdkCtx, c, []byte(t.Check))
		if err != nil {
			return false, fmt.Errorf("query %s: %w", t.Contract, err)
		}
		last = res
		var m map[string]any
		if err := json.Unmarshal(res, &m); err != nil {
			return false, err
		}
		for _, f := range t.Fields {
			if m[f] != RecoveryAddress {
				return false, nil
			}
		}
		return true, nil
	}

	for _, t := range transfers {
		c, err := addr(t.Contract)
		if err != nil {
			return fmt.Errorf("v13: contract %s: %w", t.Contract, err)
		}
		if ok, err := done(c, t); err != nil {
			return fmt.Errorf("v13: %w", err)
		} else if ok {
			continue
		}
		for _, cl := range t.Calls {
			caller := recovery
			if cl.AsOld {
				caller = old
			}
			if _, err := exec.Execute(sdkCtx, c, caller, []byte(cl.Msg), nil); err != nil {
				return fmt.Errorf("v13: execute on %s: %w", t.Contract, err)
			}
		}
		if ok, err := done(c, t); err != nil || !ok {
			return fmt.Errorf("v13: %s role not on the recovery address after transfer (err=%v, state %s)", t.Contract, err, last)
		}
		logger.Info("v13: contract role moved to recovery address", "contract", t.Contract)
	}
	return nil
}
