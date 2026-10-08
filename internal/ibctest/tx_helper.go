package ibctesting

import (
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"math/rand"
	"testing"
	"time"
)

func signAndDeliver(tb testing.TB, config client.TxConfig, app *baseapp.BaseApp,
	msgs []sdk.Msg, chainID string, numbers, sequences []uint64, _ bool,
	blockTime time.Time, nextValHash []byte, keys ...cryptotypes.PrivKey) (*abci.ResponseFinalizeBlock, error) {
	tb.Helper()
	tx, err := simtestutil.GenSignedMockTx(rand.New(rand.NewSource(1)), config, msgs,
		sdk.NewCoins(), 20000000, chainID, numbers, sequences, keys...)
	require.NoError(tb, err)
	bytes, err := config.TxEncoder()(tx)
	require.NoError(tb, err)
	return app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: app.LastBlockHeight() + 1,
		Time: blockTime, NextValidatorsHash: nextValHash, Txs: [][]byte{bytes}})
}
