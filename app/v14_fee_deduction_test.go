package app

import (
	"math/rand"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/stretchr/testify/require"
)

// v14 fee deduction. Until v14 the ante chain checked the declared fee and never
// debited it, so a fee 100x the signer's balance was accepted (private report,
// 2026-10-27). These are that proof of concept, inverted.

const v14Start int64 = 10_000_000

func v14App(t *testing.T) (*ChainApp, *secp256k1.PrivKey, sdk.AccAddress, *authtypes.BaseAccount) {
	key := secp256k1.GenPrivKey()
	sender := sdk.AccAddress(key.PubKey().Address())
	account := authtypes.NewBaseAccount(sender, key.PubKey(), 0, 0)
	pv := mock.NewPV()
	consPub, err := pv.GetPubKey()
	require.NoError(t, err)
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(consPub, 1)})
	app := SetupWithGenesisValSet(t, valSet, []authtypes.GenesisAccount{account}, chainID, nil,
		banktypes.Balance{Address: sender.String(), Coins: sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, v14Start))})
	ctx := app.NewContextLegacy(false, cmtproto.Header{Height: 1, Time: time.Now().UTC()})
	account = app.AccountKeeper.GetAccount(ctx, sender).(*authtypes.BaseAccount)
	_, err = app.Commit()
	require.NoError(t, err)
	return app, key, sender, account
}

func v14SignedSend(t *testing.T, app *ChainApp, key *secp256k1.PrivKey, acc *authtypes.BaseAccount, fee int64) sdk.Tx {
	send := banktypes.NewMsgSend(acc.GetAddress(), sdk.AccAddress([]byte("fee-test-recipient!!")),
		sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 123)))
	tx, err := simtestutil.GenSignedMockTx(rand.New(rand.NewSource(1)), app.TxConfig(), []sdk.Msg{send},
		sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, fee)), 1_000_000, chainID,
		[]uint64{acc.GetAccountNumber()}, []uint64{acc.GetSequence()}, key)
	require.NoError(t, err)
	return tx
}

func v14Deliver(t *testing.T, app *ChainApp, tx sdk.Tx) *abci.ExecTxResult {
	bz, err := app.TxConfig().TxEncoder()(tx)
	require.NoError(t, err)
	resp, err := app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: app.LastBlockHeight() + 1, Time: time.Now().UTC(), Txs: [][]byte{bz}})
	require.NoError(t, err)
	require.Len(t, resp.TxResults, 1)
	_, err = app.Commit()
	require.NoError(t, err)
	return resp.TxResults[0]
}

func v14Balances(app *ChainApp, sender sdk.AccAddress) (senderBal, feeCollector int64) {
	ctx := app.NewContextLegacy(true, cmtproto.Header{Height: app.LastBlockHeight(), Time: time.Now().UTC()})
	fc := app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	return app.BankKeeper.GetBalance(ctx, sender, sdk.DefaultBondDenom).Amount.Int64(),
		app.BankKeeper.GetBalance(ctx, fc, sdk.DefaultBondDenom).Amount.Int64()
}

func TestV14FeeIsCharged(t *testing.T) {
	app, key, sender, acc := v14App(t)
	_, fcBefore := v14Balances(app, sender)

	res := v14Deliver(t, app, v14SignedSend(t, app, key, acc, 100_000))
	require.Zero(t, res.Code, res.Log)

	bal, fc := v14Balances(app, sender)
	require.Equal(t, v14Start-123-100_000, bal, "sender pays the send and the fee")
	require.Equal(t, fcBefore+100_000, fc, "fee lands in fee_collector")
}

func TestV14FeeLargerThanBalanceIsRejected(t *testing.T) {
	app, key, sender, acc := v14App(t)
	_, fcBefore := v14Balances(app, sender)

	res := v14Deliver(t, app, v14SignedSend(t, app, key, acc, v14Start*100))
	require.NotZero(t, res.Code, "a fee 100x the balance must fail; log=%s", res.Log)
	require.Contains(t, res.Log, "insufficient funds")

	bal, fc := v14Balances(app, sender)
	require.Equal(t, v14Start, bal, "nothing moved, not even the send")
	require.Equal(t, fcBefore, fc)
}

// Exempt signers (admin, btc-relayer) keep paying nothing: the exemption jumps
// past both the globalfee check and the deduction.
func TestV14ExemptSignerPaysNoFee(t *testing.T) {
	app, key, sender, acc := v14App(t)
	nodeConfig := wasmtypes.DefaultNodeConfig()
	handler, err := NewAnteHandler(HandlerOptions{
		HandlerOptions: ante.HandlerOptions{
			AccountKeeper:   app.AccountKeeper,
			BankKeeper:      app.BankKeeper,
			SignModeHandler: app.TxConfig().SignModeHandler(),
			FeegrantKeeper:  app.FeeGrantKeeper,
			SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
		},
		IBCKeeper:             app.IBCKeeper,
		WasmConfig:            &nodeConfig,
		WasmKeeper:            &app.WasmKeeper,
		TXCounterStoreService: runtime.NewKVStoreService(app.GetKey(wasmtypes.StoreKey)),
		CircuitKeeper:         &app.CircuitKeeper,
		GlobalFeeKeeper:       app.GlobalFeeKeeper,
		BypassMinFeeMsgTypes:  GetDefaultBypassFeeMessages(),
		ExemptAddresses:       []string{sender.String()},
	})
	require.NoError(t, err)

	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: app.LastBlockHeight() + 1, Time: time.Now().UTC(), ChainID: chainID}).
		WithChainID(chainID)
	_, err = handler(ctx, v14SignedSend(t, app, key, acc, v14Start*100), false)
	require.NoError(t, err, "exempt signer is not charged, even for a fee above its balance")

	fc := app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	require.Equal(t, v14Start, app.BankKeeper.GetBalance(ctx, sender, sdk.DefaultBondDenom).Amount.Int64())
	require.True(t, app.BankKeeper.GetBalance(ctx, fc, sdk.DefaultBondDenom).Amount.IsZero())
}
