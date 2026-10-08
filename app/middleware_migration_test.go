package app

import (
	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	pfmkeeper "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/keeper"
	legacy "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/migrations/v4/legacy"
	pfmtypes "github.com/cosmos/ibc-go/v11/modules/apps/packet-forward-middleware/types"
	ratekeeper "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/keeper"
	ratetypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestLegacyMiddlewareMigrations(t *testing.T) {
	app := Setup(t)
	ctx := app.NewContextLegacy(false, cmtproto.Header{Height: 2, Time: time.Now().UTC()})
	packet := legacy.InFlightPacket{
		OriginalSenderAddress: sdk.AccAddress([]byte("source-test-account!")).String(),
		RefundChannelId:       "channel-1", RefundPortId: "transfer", RefundSequence: 7,
		PacketSrcChannelId: "channel-0", PacketSrcPortId: "transfer",
		PacketTimeoutTimestamp: uint64(time.Now().Add(time.Hour).UnixNano()), PacketTimeoutHeight: "0-100",
		PacketData:       []byte(`{"denom":"stake","amount":"123","sender":"source","receiver":"target"}`),
		RetriesRemaining: 2, Timeout: uint64(time.Minute),
	}
	key := pfmtypes.RefundPacketKey("channel-2", "transfer", 9)
	store := ctx.KVStore(app.GetKey(pfmtypes.StoreKey))
	store.Set(key, app.AppCodec().MustMarshal(&packet))
	migrator := pfmkeeper.NewMigrator(app.PacketForwardKeeper)
	require.NoError(t, migrator.Migrate3to4(ctx))
	migrated, err := app.PacketForwardKeeper.GetInflightPacket(ctx, channeltypes.Packet{
		SourceChannel: "channel-2", SourcePort: "transfer", Sequence: 9})
	require.NoError(t, err)
	require.NotNil(t, migrated)
	require.Equal(t, packet.PacketData, migrated.PacketData)
	require.Equal(t, packet.RefundSequence, migrated.RefundSequence)
	require.Equal(t, packet.OriginalSenderAddress, migrated.OriginalSenderAddress)
	require.Equal(t, packet.RetriesRemaining, migrated.RetriesRemaining)
	// Upstream deliberately refuses old nonrefundable packets; silently
	// dropping or changing their refund semantics would lose user funds.
	packet.Nonrefundable = true
	original := app.AppCodec().MustMarshal(&packet)
	store.Set(key, original)
	err = migrator.Migrate3to4(ctx)
	require.ErrorContains(t, err, "nonrefundable")
	require.Equal(t, original, store.Get(key))

	limit := ratetypes.RateLimit{
		Path:  &ratetypes.Path{Denom: "stake", ChannelOrClientId: "channel-0"},
		Quota: &ratetypes.Quota{MaxPercentSend: math.NewInt(10), MaxPercentRecv: math.NewInt(20), DurationHours: 24},
		Flow:  &ratetypes.Flow{Inflow: math.NewInt(11), Outflow: math.NewInt(12), ChannelValue: math.NewInt(1000)},
	}
	app.RatelimitKeeper.SetRateLimit(ctx, limit)
	rates := ctx.KVStore(app.GetKey(ratetypes.StoreKey))
	oldSend := append(append([]byte(nil), ratetypes.PendingSendPacketPrefix...), []byte("channel-0/7")...)
	oldReceive := append(append([]byte(nil), ratetypes.PendingReceivePacketPrefix...), []byte("channel-0/8")...)
	rates.Set(oldSend, []byte{1})
	rates.Set(oldReceive, []byte{1})
	require.NoError(t, ratekeeper.NewMigrator(app.RatelimitKeeper).Migrate1to2(ctx))
	current, found := app.RatelimitKeeper.GetRateLimit(ctx, "stake", "channel-0")
	require.True(t, found)
	require.Equal(t, limit, current, "quota and already charged flow must survive")
	require.False(t, rates.Has(oldSend))
	require.False(t, rates.Has(oldReceive))
}
