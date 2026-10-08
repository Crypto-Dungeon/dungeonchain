package app

import (
	"encoding/base64"
	"testing"
	"time"

	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	clientkeeper "github.com/cosmos/ibc-go/v10/modules/core/02-client/keeper"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	"github.com/stretchr/testify/require"
)

func TestLegacyLocalhostClientQuery(t *testing.T) {
	app := Setup(t)
	ctx := app.NewContextLegacy(false, tmproto.Header{Height: 2, Time: time.Now().UTC()})
	// IBC v10 already supplies its own built-in localhost module. The legacy
	// record must not replace that module or its current-height semantics.
	routeBefore, err := app.IBCKeeper.ClientKeeper.Route(ctx, ibcexported.LocalhostClientID)
	require.NoError(t, err)
	// Public raw IBC store record retrieved read-only from dungeon-1 at height
	// 21,546,332 on 2026-10-08. An empty fresh genesis does not expose this bug.
	raw, err := base64.StdEncoding.DecodeString("CiovaWJjLmxpZ2h0Y2xpZW50cy5sb2NhbGhvc3QudjIuQ2xpZW50U3RhdGUSCQoHCAEQv/irBw==")
	require.NoError(t, err)
	key := []byte("clients/09-localhost/clientState")
	store := ctx.KVStore(app.GetKey(ibcexported.StoreKey))
	store.Set(key, raw)
	response, err := clientkeeper.NewQueryServer(app.IBCKeeper.ClientKeeper).ClientStates(ctx, &clienttypes.QueryClientStatesRequest{})
	require.NoError(t, err)
	require.Len(t, response.ClientStates, 1)
	var state ibcexported.ClientState
	require.NoError(t, app.AppCodec().UnpackAny(response.ClientStates[0].ClientState, &state))
	legacy, ok := state.(*LocalhostClientStateV2)
	require.True(t, ok)
	require.Equal(t, clienttypes.NewHeight(1, 15399999), legacy.LatestHeight)
	require.NoError(t, legacy.Validate())
	encoded, err := app.AppCodec().MarshalJSON(response)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "ibc.lightclients.localhost.v2.ClientState")
	require.Contains(t, string(encoded), "15399999")
	require.Equal(t, raw, store.Get(key), "query must not rewrite historical state")
	routeAfter, err := app.IBCKeeper.ClientKeeper.Route(ctx, ibcexported.LocalhostClientID)
	require.NoError(t, err)
	require.IsType(t, routeBefore, routeAfter)
	require.Equal(t, clienttypes.GetSelfHeight(ctx), routeAfter.LatestHeight(ctx, ibcexported.LocalhostClientID))
	_, err = app.IBCKeeper.ClientKeeper.CreateClient(ctx, ibcexported.Localhost, []byte{}, []byte{})
	require.Error(t, err, "localhost clients cannot be created by users")
	require.Error(t, (&LocalhostClientStateV2{}).Validate())
}
