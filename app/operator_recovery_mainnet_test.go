package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreheader "cosmossdk.io/core/header"
	"cosmossdk.io/log/v2"
	"github.com/Crypto-Dungeon/dungeonchain/app/operatorrecovery"
	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13roles"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/store/v2/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/require"
)

func operatorRehearsalHome(t *testing.T) string {
	t.Helper()
	home := os.Getenv("OPERATOR_STATE_HOME")
	if home == "" {
		t.Skip("set OPERATOR_STATE_HOME to an explicitly disposable state copy")
	}
	require.True(t, strings.HasPrefix(filepath.Clean(home), "/var/tmp/dungeon-operator-rehearsal-"))
	_, err := os.Stat(filepath.Join(home, ".rehearsal-only"))
	require.NoError(t, err, "explicit disposable-copy marker required")
	for _, forbidden := range []string{"config", "keyring-test", "keyring-file", "data/priv_validator_state.json", "data/blockstore.db"} {
		_, err := os.Stat(filepath.Join(home, forbidden))
		require.True(t, os.IsNotExist(err), "signer/consensus files must not be present: %s", forbidden)
	}
	return home
}

func operatorRehearsalApp(t *testing.T, home string) *ChainApp {
	t.Helper()
	db, err := dbm.NewDB("application", dbm.PebbleDBBackend, filepath.Join(home, "data"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	loader := func(ms storetypes.CommitMultiStore) error {
		if err := ms.LoadLatestVersion(); err != nil {
			return err
		}
		if ms.LastCommitID().Version > v13.ForkHeight-1 {
			info, err := ms.(*rootmulti.Store).GetCommitInfo(v13.ForkHeight - 1)
			if err != nil {
				return err
			}
			if strings.ToUpper(hex.EncodeToString(info.Hash())) != "3FC48346799264FA7D38F83DF2B93D8E7D79BA7B012D0D1B71AA4AE0104D7862" {
				return fmt.Errorf("retained historical state is not canonical")
			}
			t.Log("restoring canonical retained pre-fork version on the marked disposable copy only")
			return ms.RollbackToVersion(v13.ForkHeight - 1)
		}
		return nil
	}
	return NewChainApp(log.NewNopLogger(), db, nil, true, simtestutil.AppOptionsMap{flags.FlagHome: home}, nil, bam.SetChainID("dungeon-1"), bam.SetStoreLoader(loader))
}

func operatorStateHashes(t *testing.T, a *ChainApp, ctx sdk.Context) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, key := range a.keys {
		h := sha256.New()
		iter := ctx.KVStore(key).Iterator(nil, nil)
		var size [8]byte
		for ; iter.Valid(); iter.Next() {
			for _, value := range [][]byte{iter.Key(), iter.Value()} {
				binary.BigEndian.PutUint64(size[:], uint64(len(value)))
				h.Write(size[:])
				h.Write(value)
			}
		}
		require.NoError(t, iter.Close())
		out[name] = hex.EncodeToString(h.Sum(nil))
	}
	return out
}

func operatorSimTx(t *testing.T, a *ChainApp, ctx sdk.Context, signer sdk.AccAddress, msg sdk.Msg) []byte {
	t.Helper()
	account := a.AccountKeeper.GetAccount(ctx, signer)
	require.NotNil(t, account)
	b := a.TxConfig().NewTxBuilder()
	require.NoError(t, b.SetMsgs(msg))
	b.SetGasLimit(2_000_000)
	b.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("udgn", 100000)))
	require.NoError(t, b.SetSignatures(signing.SignatureV2{PubKey: account.GetPubKey(), Sequence: account.GetSequence(), Data: &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: make([]byte, 64)}}))
	bz, err := a.TxConfig().TxEncoder()(b.GetTx())
	require.NoError(t, err)
	return bz
}

// This test commits synthetic blocks only to the explicitly marked disposable
// copy. It never starts a node, loads signer material, connects peers or broadcasts.
func TestOperatorRecoveryOnMainnetState(t *testing.T) {
	home := operatorRehearsalHome(t)
	a := operatorRehearsalApp(t, home)
	t.Logf("source height %d, app hash %X", a.LastBlockHeight(), a.LastCommitID().Hash)
	require.Equal(t, v13.ForkHeight-1, a.LastBlockHeight())
	anchor := "3FC48346799264FA7D38F83DF2B93D8E7D79BA7B012D0D1B71AA4AE0104D7862"
	require.Equal(t, anchor, strings.ToUpper(hex.EncodeToString(a.LastCommitID().Hash)), "copy must match historical block 22368769's prior-state app hash")
	report := map[string]any{"source_height": a.LastBlockHeight(), "source_app_hash": anchor, "live_node_changed": false, "private_keys_loaded": false, "synthetic_heights": true}
	start := time.Date(2026, 10, 26, 11, 2, 19, 719021836, time.UTC)
	contextAt := func(height int64) sdk.Context {
		tm := start.Add(time.Duration(height-v13.ForkHeight) * 6 * time.Second)
		return a.NewUncachedContext(false, cmtproto.Header{ChainID: "dungeon-1", Height: height, Time: tm}).WithHeaderInfo(coreheader.Info{ChainID: "dungeon-1", Height: height, Time: tm})
	}
	proposer, err := hex.DecodeString("27FA426650F329594882B99A5C0E3CE4BB94BEA0")
	require.NoError(t, err)
	advance := func(height int64) {
		t.Logf("FinalizeBlock/Commit on disposable copy at synthetic height %d", height)
		_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: height, Time: contextAt(height).BlockTime(), ProposerAddress: proposer})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
	}
	advance(v13.ForkHeight)
	advance(v13.ForkHeight + 1)
	advance(v13.ForkHeight + 2) // queued consensus rotations apply through EndBlock
	roleHeight := v13.ForkHeight + 3
	operatorHeight := roleHeight + 1
	// Direct scheduling immediately before the activation block models the
	// approved plan being present when Cosmovisor switches binaries. The SDK
	// correctly rejects running a handler-bearing binary ahead of its height.
	require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(contextAt(roleHeight-1), upgradetypes.Plan{Name: v13roles.Name, Height: roleHeight}))
	advance(roleHeight)
	ctx := contextAt(operatorHeight)
	opBytes, err := a.AccountKeeper.AddressCodec().StringToBytes(operatorrecovery.Operator)
	require.NoError(t, err)
	ctrlBytes, err := a.AccountKeeper.AddressCodec().StringToBytes(operatorrecovery.Controller)
	require.NoError(t, err)
	op, controller := sdk.AccAddress(opBytes), sdk.AccAddress(ctrlBytes)
	account := a.AccountKeeper.GetAccount(ctx, controller)
	require.NotNil(t, account)
	if account.GetPubKey() == nil {
		// Historical copy predates the recovery wallet's first signed tx.
		// Project only its now-public, independently possession-proven key.
		pub, err := base64.StdEncoding.DecodeString(operatorrecovery.ControllerPublicKey)
		require.NoError(t, err)
		key := &secp256k1.PubKey{Key: pub}
		require.Equal(t, []byte(controller), []byte(key.Address()))
		require.NoError(t, account.SetPubKey(key))
		a.AccountKeeper.SetAccount(ctx, account)
		report["public_key_projection"] = "Only the existing controller's public key was set on the historical copy, matching current chain state and verified wallet proof."
	}
	before := operatorStateHashes(t, a, ctx)
	val, err := a.StakingKeeper.GetValidator(ctx, sdk.ValAddress(op))
	require.NoError(t, err)
	validatorBefore := a.AppCodec().MustMarshal(&val)
	trial, _ := ctx.CacheContext()
	plan := upgradetypes.Plan{Name: operatorrecovery.Name, Height: ctx.BlockHeight()}
	require.NoError(t, a.UpgradeKeeper.ApplyUpgrade(trial, plan))
	after := operatorStateHashes(t, a, trial)
	for name, hash := range before {
		if name != "authz" && name != "distribution" && name != "upgrade" {
			require.Equal(t, hash, after[name], "unrelated store changed: %s", name)
		}
	}
	withdraw, err := a.DistrKeeper.GetDelegatorWithdrawAddr(trial, op)
	require.NoError(t, err)
	require.Equal(t, controller, withdraw)
	// Distribution's entire keyspace may change only at this operator's
	// withdrawal-address key; commissions, rewards and all other users survive.
	distKey := a.keys[distrtypes.StoreKey]
	allowedDistKey := distrtypes.GetDelegatorWithdrawAddrKey(op)
	for _, pair := range [][2]sdk.Context{{ctx, trial}, {trial, ctx}} {
		iter := pair[0].KVStore(distKey).Iterator(nil, nil)
		for ; iter.Valid(); iter.Next() {
			if !bytes.Equal(iter.Key(), allowedDistKey) {
				require.Equal(t, iter.Value(), pair[1].KVStore(distKey).Get(iter.Key()), "unrelated distribution key changed")
			}
		}
		require.NoError(t, iter.Close())
	}
	for _, kind := range operatorrecovery.MessageTypes() {
		authorization, expiry := a.AuthzKeeper.GetAuthorization(trial, controller, op, kind)
		require.NotNil(t, authorization, kind)
		require.Nil(t, expiry)
	}
	val, err = a.StakingKeeper.GetValidator(trial, sdk.ValAddress(op))
	require.NoError(t, err)
	require.Equal(t, validatorBefore, a.AppCodec().MustMarshal(&val))
	report["handler_only_before_store_hashes"], report["handler_only_after_store_hashes"] = before, after
	// Discard the trial; execute the registered upgrade in the real ABCI path.
	require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(contextAt(roleHeight), plan))
	advance(operatorHeight)
	ctx = contextAt(operatorHeight)
	done, err := a.UpgradeKeeper.GetDoneHeight(ctx, operatorrecovery.Name)
	require.NoError(t, err)
	require.Equal(t, operatorHeight, done)
	report["final_committed_height"] = a.LastBlockHeight()
	report["final_committed_app_hash"] = strings.ToUpper(hex.EncodeToString(a.LastCommitID().Hash))
	// Full BaseApp simulation includes fee handling, sequence checks and the
	// actual ante-to-message context handoff; cryptographic signing is skipped
	// only by standard SDK simulation mode. The wallet proof covers possession.
	desc := stakingtypes.Description{Moniker: val.Description.Moniker, Identity: stakingtypes.DoNotModifyDesc, Website: stakingtypes.DoNotModifyDesc, SecurityContact: stakingtypes.DoNotModifyDesc, Details: stakingtypes.DoNotModifyDesc}
	edit := authz.NewMsgExec(controller, []sdk.Msg{&stakingtypes.MsgEditValidator{ValidatorAddress: operatorrecovery.Validator, Description: desc}})
	editBytes := operatorSimTx(t, a, ctx, controller, &edit)
	gas, _, err := a.Simulate(editBytes)
	require.NoError(t, err, "full SDK management simulation")
	report["full_management_simulation_gas"] = gas.GasUsed
	check, err := a.CheckTx(&abci.RequestCheckTx{Tx: editBytes, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.NotZero(t, check.Code, "bogus signature must fail outside simulation mode")
	require.Contains(t, check.Log, "signature", "failure must reach real signature verification")
	report["bogus_signature_rejected"] = true
	oldDirect := &stakingtypes.MsgEditValidator{ValidatorAddress: operatorrecovery.Validator, Description: desc}
	_, _, err = a.Simulate(operatorSimTx(t, a, ctx, op, oldDirect))
	require.ErrorContains(t, err, "frozen")
	wrongRecipient := authz.NewMsgExec(controller, []sdk.Msg{banktypes.NewMsgSend(op, op, sdk.NewCoins(sdk.NewInt64Coin("udgn", 1)))})
	_, _, err = a.Simulate(operatorSimTx(t, a, ctx, controller, &wrongRecipient))
	require.ErrorContains(t, err, "frozen")
	// A separately labeled local voting fixture exercises the real vote path.
	fixtureID := uint64(9_000_045)
	require.NoError(t, a.GovKeeper.SetProposal(ctx, govtypes.Proposal{Id: fixtureID, Status: govtypes.StatusVotingPeriod}))
	require.NoError(t, a.GovKeeper.VotingPeriodProposals.Set(ctx, fixtureID, []byte{1}))
	vote := authz.NewMsgExec(controller, []sdk.Msg{&govtypes.MsgVote{ProposalId: fixtureID, Voter: op.String(), Option: govtypes.OptionYes}})
	gas, _, err = a.Simulate(operatorSimTx(t, a, ctx, controller, &vote))
	require.NoError(t, err, "full SDK voting simulation on explicitly synthetic proposal")
	report["synthetic_vote_simulation_gas"] = gas.GasUsed
	// Rewards are preserved by the handler, including this exact record.
	commission, err := a.DistrKeeper.GetValidatorAccumulatedCommission(ctx, sdk.ValAddress(op))
	require.NoError(t, err)
	report["commission_record_preserved"] = commission.Commission.String()
	report["distribution_withdraw_key"] = hex.EncodeToString(distrtypes.GetDelegatorWithdrawAddrKey(op))
	report["passed"] = true
	report["completed_at_utc"] = time.Now().UTC().Format(time.RFC3339)
	if output := os.Getenv("OPERATOR_REHEARSAL_REPORT"); output != "" {
		bz, err := json.MarshalIndent(report, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(output, append(bz, '\n'), 0600))
	}
}
