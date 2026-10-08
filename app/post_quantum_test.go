package app

import (
	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	txsigning "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"math/rand"
	"testing"
	"time"

	"cosmossdk.io/math"
	cryptoenc "github.com/cometbft/cometbft/crypto/encoding"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/mldsa65"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// Exercise Dungeon's actual staking, bank, distribution and slashing keepers.
// Consensus activation is explicit; installing the new binary alone retains
// the legacy key allowance and must reject a PQ rotation.
func TestPostQuantumValidatorRotation(t *testing.T) {
	app := Setup(t)
	now := time.Now().UTC()
	ctx := app.NewContextLegacy(false, cmtproto.Header{Height: 2, Time: now})
	cpBefore := ctx.ConsensusParams()
	cpBefore.Validator = &cmtproto.ValidatorParams{PubKeyTypes: []string{"ed25519"}}
	ctx = ctx.WithConsensusParams(cpBefore)
	validators, err := app.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.Len(t, validators, 1)
	validator := validators[0]
	operator, err := sdk.ValAddressFromBech32(validator.OperatorAddress)
	require.NoError(t, err)
	oldAddr, err := validator.GetConsAddr()
	require.NoError(t, err)
	params, err := app.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	require.Equal(t, params.BondDenom, params.KeyRotationFee.Denom)
	require.True(t, params.KeyRotationFee.Amount.Equal(math.NewInt(1000000)))
	initAccountWithCoins(app, ctx, sdk.AccAddress(operator), sdk.NewCoins(sdk.NewInt64Coin(params.BondDenom, 10000000)))
	supplyBefore := app.BankKeeper.GetSupply(ctx, params.BondDenom)
	key, err := mldsa65.GenPrivKey()
	require.NoError(t, err)
	pub, err := types.NewAnyWithValue(key.PubKey())
	require.NoError(t, err)
	msg := &stakingtypes.MsgRotateConsPubKey{ValidatorAddress: operator.String(), NewPubkey: pub}
	server := stakingkeeper.NewMsgServerImpl(app.StakingKeeper)
	_, err = server.RotateConsPubKey(ctx, msg)
	require.Error(t, err, "PQ rotation must remain disabled before consensus activation")
	require.Equal(t, supplyBefore, app.BankKeeper.GetSupply(ctx, params.BondDenom))

	cp := ctx.ConsensusParams()
	cp.Validator = &cmtproto.ValidatorParams{PubKeyTypes: []string{"ed25519", "ml_dsa_65"}}
	ctx = ctx.WithConsensusParams(cp)
	require.NoError(t, app.ConsensusParamsKeeper.ParamsStore.Set(ctx, cp))
	_, err = server.RotateConsPubKey(ctx, msg)
	require.NoError(t, err)
	supplyAfter := app.BankKeeper.GetSupply(ctx, params.BondDenom)
	require.True(t, supplyBefore.Amount.Sub(supplyAfter.Amount).Equal(params.KeyRotationFee.Amount), "rotation fee must burn")
	pending, err := app.StakingKeeper.PendingConsKeyRotations(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	updates, err := app.StakingKeeper.EndBlocker(ctx)
	require.NoError(t, err)
	foundPQ := false
	for _, update := range updates {
		pk, err := cryptoenc.PubKeyFromProto(update.PubKey)
		require.NoError(t, err)
		if pk.Type() == "ml_dsa_65" {
			foundPQ = true
			require.Positive(t, update.Power)
		}
	}
	require.True(t, foundPQ, "Comet must receive the new consensus key")
	ctx = ctx.WithBlockHeight(2 + stakingtypes.ConsensusUpdateDelay)
	_, err = app.StakingKeeper.EndBlocker(ctx)
	require.NoError(t, err)
	rotated, err := app.StakingKeeper.GetValidator(ctx, operator)
	require.NoError(t, err)
	require.Equal(t, validator.Status, rotated.Status)
	require.Equal(t, validator.Tokens, rotated.Tokens)
	require.Equal(t, validator.DelegatorShares, rotated.DelegatorShares)
	require.Equal(t, pub.TypeUrl, rotated.ConsensusPubkey.TypeUrl)
	historical, err := app.StakingKeeper.ValidatorByHistoricalConsAddr(ctx, oldAddr)
	require.NoError(t, err)
	require.Equal(t, operator.String(), historical.GetOperator())
	newAddr, err := rotated.GetConsAddr()
	require.NoError(t, err)
	_, err = app.SlashingKeeper.GetValidatorSigningInfo(ctx, newAddr)
	require.NoError(t, err, "slashing tracking must follow the rotated key")
	histories, err := app.StakingKeeper.ExportConsKeyRotationHistory(ctx)
	require.NoError(t, err)
	require.Len(t, histories, 1)
	require.Equal(t, sdk.ConsAddress(oldAddr).String(), histories[0].OldConsensusAddress)
	another, err := mldsa65.GenPrivKey()
	require.NoError(t, err)
	next, err := types.NewAnyWithValue(another.PubKey())
	require.NoError(t, err)
	_, err = server.RotateConsPubKey(ctx, &stakingtypes.MsgRotateConsPubKey{ValidatorAddress: operator.String(), NewPubkey: next})
	require.ErrorIs(t, err, stakingtypes.ErrExceedingMaxConsPubKeyRotations)
}

// Verify the full Dungeon ante chain, including ML-DSA gas charging and
// authentication. A valid signature commits; replay and corruption cannot
// move funds. This does not bypass authentication through keeper calls.
func TestPostQuantumAccountTransactions(t *testing.T) {
	for _, executor := range []string{"sequential", "block-stm"} {
		t.Run(executor, func(t *testing.T) { testPostQuantumAccountTransactions(t, executor) })
	}
}
func testPostQuantumAccountTransactions(t *testing.T, executor string) {
	key, err := mldsa65.GenPrivKey()
	require.NoError(t, err)
	sender := sdk.AccAddress(key.PubKey().Address())
	recipient := sdk.AccAddress([]byte("pq-transfer-target!!"))
	account := authtypes.NewBaseAccount(sender, key.PubKey(), 0, 0)
	pv := mock.NewPV()
	consensusKey, err := pv.GetPubKey()
	require.NoError(t, err)
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(consensusKey, 1)})
	app := SetupWithGenesisValSetAndOptions(t, valSet, []authtypes.GenesisAccount{account}, chainID, nil,
		simtestutil.AppOptionsMap{server.FlagBlockExecutor: executor, server.FlagBlockSTMWorkers: 4, server.FlagBlockSTMPreEstimate: true},
		banktypes.Balance{Address: sender.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 10000000))})
	ctx := app.NewContextLegacy(false, cmtproto.Header{Height: 1, Time: time.Now().UTC()})
	account = app.AccountKeeper.GetAccount(ctx, sender).(*authtypes.BaseAccount)
	_, err = app.Commit()
	require.NoError(t, err)
	send := banktypes.NewMsgSend(sender, recipient, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 123)))
	makeTx := func(sequence uint64, corrupt bool) []byte {
		tx, err := simtestutil.GenSignedMockTx(rand.New(rand.NewSource(1)), app.TxConfig(), []sdk.Msg{send},
			sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 100000)), 1000000, chainID,
			[]uint64{account.GetAccountNumber()}, []uint64{sequence}, &key)
		require.NoError(t, err)
		if corrupt {
			signatures, err := tx.(authsigning.SigVerifiableTx).GetSignaturesV2()
			require.NoError(t, err)
			signatures[0].Data.(*txsigning.SingleSignatureData).Signature[0] ^= 0xff
			builder := app.TxConfig().NewTxBuilder()
			require.NoError(t, builder.SetMsgs(send))
			builder.SetFeeAmount(tx.(sdk.FeeTx).GetFee())
			builder.SetGasLimit(tx.(sdk.FeeTx).GetGas())
			builder.SetMemo(tx.(sdk.TxWithMemo).GetMemo())
			require.NoError(t, builder.SetSignatures(signatures...))
			tx = builder.GetTx()
		}
		bz, err := app.TxConfig().TxEncoder()(tx)
		require.NoError(t, err)
		return bz
	}
	deliver := func(bz []byte) *abci.ExecTxResult {
		response, err := app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: app.LastBlockHeight() + 1, Time: time.Now().UTC(), Txs: [][]byte{bz},
		})
		require.NoError(t, err)
		require.Len(t, response.TxResults, 1)
		_, err = app.Commit()
		require.NoError(t, err)
		return response.TxResults[0]
	}
	valid := makeTx(0, false)
	result := deliver(valid)
	require.Zero(t, result.Code, result.Log)
	require.Positive(t, result.GasUsed)
	replay := deliver(valid)
	require.NotZero(t, replay.Code, "replay must fail account sequence checks")
	forged := deliver(makeTx(1, true))
	require.NotZero(t, forged.Code, "modified ML-DSA signature must be rejected")
	ctx = app.NewContextLegacy(true, cmtproto.Header{Height: app.LastBlockHeight(), Time: time.Now().UTC()})
	require.Equal(t, "123", app.BankKeeper.GetBalance(ctx, recipient, sdk.DefaultBondDenom).Amount.String())
	require.Equal(t, uint64(1), app.AccountKeeper.GetAccount(ctx, sender).GetSequence())
}
