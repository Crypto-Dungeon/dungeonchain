// Package v13operator is a draft, separately governed recovery. It is not part
// of v13.0.2 or proposal 45 and has no automatic activation height.
package v13operator

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	"github.com/Crypto-Dungeon/dungeonchain/app/operatorrecovery"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13roles"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/authz"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

var Upgrade = upgrades.Upgrade{UpgradeName: operatorrecovery.Name, CreateUpgradeHandler: CreateUpgradeHandler}

type Target struct {
	Operator, Validator, Controller                            string
	OperatorPublicKey, ControllerPublicKey, ConsensusPublicKey string
}

func MainnetTarget() Target {
	return Target{operatorrecovery.Operator, operatorrecovery.Validator, operatorrecovery.Controller,
		operatorrecovery.OperatorPublicKey, operatorrecovery.ControllerPublicKey, operatorrecovery.ConsensusPublicKey}
}

func CreateUpgradeHandler(_ upgrades.ModuleManager, _ module.Configurator, keepers *upgrades.AppKeepers) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, plan upgradetypes.Plan, vm module.VersionMap) (module.VersionMap, error) {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		if sdkCtx.ChainID() != "dungeon-1" || plan.Name != operatorrecovery.Name || plan.Height != sdkCtx.BlockHeight() {
			return nil, fmt.Errorf("operator recovery requires its approved dungeon-1 plan at the scheduled height")
		}
		if keepers == nil || keepers.UpgradeKeeper == nil {
			return nil, fmt.Errorf("operator recovery upgrade keeper is missing")
		}
		previous, err := keepers.UpgradeKeeper.GetDoneHeight(ctx, v13roles.Name)
		if err != nil {
			return nil, err
		}
		if previous <= 0 || previous >= sdkCtx.BlockHeight() {
			return nil, fmt.Errorf("contract-role upgrade must complete before operator recovery")
		}
		if err := Recover(sdkCtx, keepers, MainnetTarget()); err != nil {
			return nil, err
		}
		return vm, nil
	}
}

func Recover(ctx sdk.Context, k *upgrades.AppKeepers, target Target) error {
	if k == nil || k.AccountKeeper == nil || k.StakingKeeper == nil || k.DistrKeeper == nil || k.AuthzKeeper == nil || k.Codec == nil {
		return fmt.Errorf("operator recovery keepers are incomplete")
	}
	op, err := k.AccountKeeper.AddressCodec().StringToBytes(target.Operator)
	if err != nil {
		return err
	}
	controller, err := k.AccountKeeper.AddressCodec().StringToBytes(target.Controller)
	if err != nil {
		return err
	}
	valAddr, err := k.StakingKeeper.ValidatorAddressCodec().StringToBytes(target.Validator)
	if err != nil {
		return err
	}
	if !bytes.Equal(op, valAddr) || bytes.Equal(op, controller) {
		return fmt.Errorf("invalid operator recovery binding")
	}
	for _, check := range []struct {
		address []byte
		key     string
	}{{op, target.OperatorPublicKey}, {controller, target.ControllerPublicKey}} {
		account := k.AccountKeeper.GetAccount(ctx, check.address)
		want, err := base64.StdEncoding.DecodeString(check.key)
		if err != nil {
			return err
		}
		if account == nil || account.GetPubKey() == nil || account.GetPubKey().Type() != "secp256k1" || !bytes.Equal(account.GetPubKey().Bytes(), want) || !bytes.Equal(account.GetPubKey().Address(), check.address) {
			return fmt.Errorf("operator recovery account/public-key precondition failed")
		}
	}
	val, err := k.StakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return err
	}
	var consensus cryptotypes.PubKey
	if err := k.Codec.UnpackAny(val.ConsensusPubkey, &consensus); err != nil {
		return err
	}
	want, err := base64.StdEncoding.DecodeString(target.ConsensusPublicKey)
	if err != nil {
		return err
	}
	if consensus.Type() != "ed25519" || !bytes.Equal(consensus.Bytes(), want) {
		return fmt.Errorf("operator recovery consensus-key precondition failed")
	}
	withdraw, err := k.DistrKeeper.GetDelegatorWithdrawAddr(ctx, op)
	if err != nil {
		return err
	}
	if !bytes.Equal(withdraw, op) && !bytes.Equal(withdraw, controller) {
		return fmt.Errorf("unexpected operator withdrawal destination")
	}

	cache, write := ctx.CacheContext()
	// Stale delegations made with the compromised key must not survive the
	// recovery. Collect before deleting; never mutate an open grant iterator.
	type oldGrant struct {
		grantee sdk.AccAddress
		msgType string
	}
	var stale []oldGrant
	var grantErr error
	k.AuthzKeeper.IterateGrants(cache, func(granter, grantee sdk.AccAddress, grant authz.Grant) bool {
		if !bytes.Equal(granter, op) {
			return false
		}
		var authorization authz.Authorization
		if err := k.Codec.UnpackAny(grant.Authorization, &authorization); err != nil {
			grantErr = err
			return true
		}
		stale = append(stale, oldGrant{bytes.Clone(grantee), authorization.MsgTypeURL()})
		return false
	})
	if grantErr != nil {
		return grantErr
	}
	for _, grant := range stale {
		if err := k.AuthzKeeper.DeleteGrant(cache, grant.grantee, op, grant.msgType); err != nil {
			return err
		}
	}
	if err := k.DistrKeeper.SetWithdrawAddr(cache, op, controller); err != nil {
		return err
	}
	for _, msgType := range operatorrecovery.MessageTypes() {
		if err := k.AuthzKeeper.SaveGrant(cache, controller, op, authz.NewGenericAuthorization(msgType), nil); err != nil {
			return err
		}
	}
	// Neither the validator object, delegations, balances, signing keys nor any
	// other frozen account is modified. Runtime authz is restricted by both ante and the execution guard.
	write()
	return nil
}
