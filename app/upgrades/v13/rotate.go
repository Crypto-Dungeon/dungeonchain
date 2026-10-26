package v13

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
)

// KeyRotation moves one validator to a new ed25519 consensus key.
type KeyRotation struct {
	Validator string // dungeonvaloper1…
	NewPubKey string // base64 ed25519 pubkey (priv_validator_key.json pub_key.value)
}

// Rotations are the three operator validators halted on 2026-10-26. Their old
// consensus keys sat on machines the intruder reached (crypto7 directly), so
// each moves to a fresh key generated on its own node during the halt and
// never copied anywhere (config/priv_validator_key.json.NEW-v13).
var Rotations = []KeyRotation{
	{"dungeonvaloper1pkr96cu09pzug2m2w2h97u5snpq3spscvfjwpz", "HiAZoqwCqhWhjQYwybs3k4lrMOdX/qknD+njZikhJ0I="}, // The Hive, crypto1
	{"dungeonvaloper1aj5jlmvqp8dd26rsec6624szthlazdn25leye3", "i+lpF0YXMv1yEO/91obZnI2C4QElB/S+L4G6N+dcqNQ="}, // CryptoDungeon, crypto4
	{"dungeonvaloper19qp6fk7lj4y36djnwkvpv4j7pv8x3epcpppugd", "kHlJd6TnZebgXSBpEdiyyLQEUXdLYiTmzsvSaEgTM6A="}, // House of the Holy, crypto7
}

// RotateKeys queues each rotation through the staking msg server, so the SDK's
// own checks (key type, reuse, jailed, one per unbonding window) and its
// apply-at-H+delay schedule all hold. The key rotation fee is pre-funded from
// RecoveryAddress so a drained operator account cannot fail the upgrade.
// Old keys keep signing until the rotation applies; then each node swaps to
// its .NEW-v13 key file. Safe to run twice: an already-rotated validator is
// skipped.
func RotateKeys(ctx context.Context, ak *upgrades.AppKeepers, rotations []KeyRotation) error {
	logger := sdk.UnwrapSDKContext(ctx).Logger().With("upgrade", UpgradeName)
	b, err := ak.AccountKeeper.AddressCodec().StringToBytes(RecoveryAddress)
	if err != nil {
		return fmt.Errorf("v13: recovery address: %w", err)
	}
	payer := sdk.AccAddress(b)
	params, err := ak.StakingKeeper.GetParams(ctx)
	if err != nil {
		return err
	}
	server := stakingkeeper.NewMsgServerImpl(ak.StakingKeeper)

	for _, r := range rotations {
		pk, err := decodeEd25519(r.NewPubKey)
		if err != nil {
			return fmt.Errorf("v13: %s new key: %w", r.Validator, err)
		}
		valBz, err := ak.StakingKeeper.ValidatorAddressCodec().StringToBytes(r.Validator)
		if err != nil {
			return fmt.Errorf("v13: validator %s: %w", r.Validator, err)
		}
		val, err := ak.StakingKeeper.GetValidator(ctx, valBz)
		if err != nil {
			return fmt.Errorf("v13: validator %s: %w", r.Validator, err)
		}
		if cur, ok := val.ConsensusPubkey.GetCachedValue().(cryptotypes.PubKey); ok && bytes.Equal(cur.Bytes(), pk.Bytes()) {
			continue
		}
		if done, err := ak.StakingKeeper.HasConsKeyRotationInUnbondingWindow(ctx, valBz); err != nil {
			return err
		} else if done {
			logger.Info("v13: rotation already queued, skipping", "validator", r.Validator)
			continue
		}

		fee := sdk.NewCoins(params.KeyRotationFee)
		if err := ak.BankKeeper.SendCoins(ctx, payer, sdk.AccAddress(valBz), fee); err != nil {
			return fmt.Errorf("v13: fund rotation fee for %s: %w", r.Validator, err)
		}
		anyPk, err := codectypes.NewAnyWithValue(pk)
		if err != nil {
			return err
		}
		if _, err := server.RotateConsPubKey(ctx, &stakingtypes.MsgRotateConsPubKey{ValidatorAddress: r.Validator, NewPubkey: anyPk}); err != nil {
			return fmt.Errorf("v13: rotate %s: %w", r.Validator, err)
		}
		logger.Info("v13: consensus key rotation queued", "validator", r.Validator, "new_cons_addr", sdk.ConsAddress(pk.Address()).String())
	}
	return nil
}

func decodeEd25519(b64 string) (*ed25519.PubKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PubKeySize {
		return nil, fmt.Errorf("want %d bytes, got %d", ed25519.PubKeySize, len(raw))
	}
	return &ed25519.PubKey{Key: raw}, nil
}
