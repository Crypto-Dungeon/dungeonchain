package app_test

import (
	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"
	"encoding/json"
	dungeonapp "github.com/Crypto-Dungeon/dungeonchain/app"
	ibctesting "github.com/Crypto-Dungeon/dungeonchain/internal/ibctest"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ratetypes "github.com/cosmos/ibc-go/v11/modules/apps/rate-limiting/types"
	transfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v11/modules/core/04-channel/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func dungeonCoordinator(t *testing.T, count int) *ibctesting.Coordinator {
	t.Helper()
	return ibctesting.NewCustomAppCoordinator(t, count, func() (ibctesting.TestingApp, map[string]json.RawMessage) {
		options := simtestutil.NewAppOptionsWithFlagHome(t.TempDir())
		app := dungeonapp.NewChainApp(log.NewNopLogger(), dbm.NewMemDB(), nil, true, options, nil)
		return app, app.DefaultGenesis()
	})
}
func TestDungeonIBCTransfer(t *testing.T) {
	coordinator := dungeonCoordinator(t, 2)
	a, b := coordinator.GetChain(ibctesting.GetChainID(1)), coordinator.GetChain(ibctesting.GetChainID(2))
	path := ibctesting.NewPath(a, b)
	path.EndpointA.ChannelConfig.PortID = transfertypes.PortID
	path.EndpointB.ChannelConfig.PortID = transfertypes.PortID
	path.EndpointA.ChannelConfig.Version = transfertypes.V1
	path.EndpointB.ChannelConfig.Version = transfertypes.V1
	coordinator.Setup(path)
	amount := sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(12345))
	msg := transfertypes.NewMsgTransfer(transfertypes.PortID, path.EndpointA.ChannelID, amount,
		a.SenderAccount.GetAddress().String(), b.SenderAccount.GetAddress().String(),
		clienttypes.ZeroHeight(), uint64(coordinator.CurrentTime.Add(time.Hour).UnixNano()), "")
	result, err := a.SendMsgs(msg)
	require.NoError(t, err)
	packets, err := ibctesting.ParsePacketsFromEvents(channeltypes.EventTypeSendPacket, result.Events)
	require.NoError(t, err)
	require.Len(t, packets, 1)
	require.NoError(t, path.RelayPacket(packets[0]))
	denom := transfertypes.NewDenom(sdk.DefaultBondDenom,
		transfertypes.NewHop(transfertypes.PortID, path.EndpointB.ChannelID)).IBCDenom()
	balance := b.GetSimApp().BankKeeper.GetBalance(b.GetContext(), b.SenderAccount.GetAddress(), denom)
	require.Equal(t, amount.Amount, balance.Amount)
	// Successful acknowledgement deletes the source commitment.
	commitment := a.GetSimApp().IBCKeeper.ChannelKeeper.GetPacketCommitment(a.GetContext(),
		packets[0].SourcePort, packets[0].SourceChannel, packets[0].Sequence)
	require.Empty(t, commitment)
	// Reverse the voucher and confirm escrow is released on its origin chain.
	back := transfertypes.NewMsgTransfer(transfertypes.PortID, path.EndpointB.ChannelID,
		sdk.NewCoin(denom, amount.Amount), b.SenderAccount.GetAddress().String(),
		a.SenderAccount.GetAddress().String(), clienttypes.ZeroHeight(),
		uint64(coordinator.CurrentTime.Add(time.Hour).UnixNano()), "")
	result, err = b.SendMsgs(back)
	require.NoError(t, err)
	packets, err = ibctesting.ParsePacketsFromEvents(channeltypes.EventTypeSendPacket, result.Events)
	require.NoError(t, err)
	require.NoError(t, path.RelayPacket(packets[0]))
	require.True(t, b.GetSimApp().BankKeeper.GetBalance(b.GetContext(), b.SenderAccount.GetAddress(), denom).IsZero())
}

// Exercise the transfer -> PFM -> rate-limit -> channel ordering with three
// Dungeon apps. Forwarded packets must consume quota and acknowledge/refund
// their original source; PFM must never bypass rate limiting.
func TestDungeonForwardingRespectsRateLimit(t *testing.T) {
	coordinator := dungeonCoordinator(t, 3)
	a, b, c := coordinator.GetChain(ibctesting.GetChainID(1)), coordinator.GetChain(ibctesting.GetChainID(2)), coordinator.GetChain(ibctesting.GetChainID(3))
	connect := func(a, b *ibctesting.TestChain) *ibctesting.Path {
		path := ibctesting.NewPath(a, b)
		path.EndpointA.ChannelConfig.PortID = transfertypes.PortID
		path.EndpointB.ChannelConfig.PortID = transfertypes.PortID
		path.EndpointA.ChannelConfig.Version = transfertypes.V1
		path.EndpointB.ChannelConfig.Version = transfertypes.V1
		coordinator.Setup(path)
		return path
	}
	ab, bc := connect(a, b), connect(b, c)
	intermediate := transfertypes.NewDenom(sdk.DefaultBondDenom,
		transfertypes.NewHop(transfertypes.PortID, ab.EndpointB.ChannelID))
	limit := ratetypes.RateLimit{
		Path:  &ratetypes.Path{Denom: intermediate.IBCDenom(), ChannelOrClientId: bc.EndpointA.ChannelID},
		Quota: &ratetypes.Quota{MaxPercentSend: math.NewInt(10), MaxPercentRecv: math.NewInt(100), DurationHours: 24},
		Flow:  &ratetypes.Flow{Inflow: math.ZeroInt(), Outflow: math.ZeroInt(), ChannelValue: math.NewInt(10000)},
	}
	b.GetSimApp().RatelimitKeeper.SetRateLimit(b.GetContext(), limit)
	coordinator.CommitBlock(b)
	memo, err := json.Marshal(map[string]any{"forward": map[string]any{
		"receiver": c.SenderAccount.GetAddress().String(), "port": transfertypes.PortID, "channel": bc.EndpointA.ChannelID}})
	require.NoError(t, err)
	send := func(amount int64) channeltypes.Packet {
		msg := transfertypes.NewMsgTransfer(transfertypes.PortID, ab.EndpointA.ChannelID,
			sdk.NewInt64Coin(sdk.DefaultBondDenom, amount), a.SenderAccount.GetAddress().String(), "pfm",
			clienttypes.ZeroHeight(), uint64(coordinator.CurrentTime.Add(time.Hour).UnixNano()), string(memo))
		result, err := a.SendMsgs(msg)
		require.NoError(t, err)
		packets, err := ibctesting.ParsePacketsFromEvents(channeltypes.EventTypeSendPacket, result.Events)
		require.NoError(t, err)
		require.Len(t, packets, 1)
		return packets[0]
	}
	original := send(999)
	require.NoError(t, ab.EndpointB.UpdateClient())
	result, err := ab.EndpointB.RecvPacketWithResult(original)
	require.NoError(t, err)
	forwarded, err := ibctesting.ParsePacketsFromEvents(channeltypes.EventTypeSendPacket, result.Events)
	require.NoError(t, err)
	require.Len(t, forwarded, 1)
	_, ack, err := bc.RelayPacketWithResults(forwarded[0])
	require.NoError(t, err)
	require.NoError(t, ab.EndpointA.UpdateClient())
	require.NoError(t, ab.EndpointA.AcknowledgePacket(original, ack))
	denom := transfertypes.NewDenom(sdk.DefaultBondDenom,
		transfertypes.NewHop(transfertypes.PortID, bc.EndpointB.ChannelID),
		transfertypes.NewHop(transfertypes.PortID, ab.EndpointB.ChannelID)).IBCDenom()
	require.Equal(t, "999", c.GetSimApp().BankKeeper.GetBalance(c.GetContext(), c.SenderAccount.GetAddress(), denom).Amount.String())
	inflight, err := b.GetSimApp().PacketForwardKeeper.GetInflightPacket(b.GetContext(), forwarded[0])
	require.NoError(t, err)
	require.Nil(t, inflight)
	current, found := b.GetSimApp().RatelimitKeeper.GetRateLimit(b.GetContext(), intermediate.IBCDenom(), bc.EndpointA.ChannelID)
	require.True(t, found)
	require.Equal(t, "999", current.Flow.Outflow.String())
	balanceBefore := a.GetSimApp().BankKeeper.GetBalance(a.GetContext(), a.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	overQuota := send(2000)
	_, errorAck, err := ab.RelayPacketWithResults(overQuota)
	require.NoError(t, err)
	var parsed channeltypes.Acknowledgement
	require.NoError(t, json.Unmarshal(errorAck, &parsed))
	require.False(t, parsed.Success(), "forwarded packet over quota must fail")
	require.Equal(t, balanceBefore, a.GetSimApp().BankKeeper.GetBalance(a.GetContext(), a.SenderAccount.GetAddress(), sdk.DefaultBondDenom), "failed forwarding must refund the source")
	require.Equal(t, "999", c.GetSimApp().BankKeeper.GetBalance(c.GetContext(), c.SenderAccount.GetAddress(), denom).Amount.String())
}

func TestDungeonIBCV2Transfer(t *testing.T) {
	coordinator := dungeonCoordinator(t, 2)
	a, b := coordinator.GetChain(ibctesting.GetChainID(1)), coordinator.GetChain(ibctesting.GetChainID(2))
	path := ibctesting.NewPath(a, b)
	path.SetupV2()
	a.GetSimApp().RatelimitKeeper.SetRateLimit(a.GetContext(), ratetypes.RateLimit{
		Path:  &ratetypes.Path{Denom: sdk.DefaultBondDenom, ChannelOrClientId: path.EndpointA.ClientID},
		Quota: &ratetypes.Quota{MaxPercentSend: math.NewInt(10), MaxPercentRecv: math.NewInt(100), DurationHours: 24},
		Flow:  &ratetypes.Flow{Inflow: math.ZeroInt(), Outflow: math.ZeroInt(), ChannelValue: math.NewInt(10000)},
	})
	coordinator.CommitBlock(a)
	msg := transfertypes.NewMsgTransferWithEncoding(transfertypes.PortID, path.EndpointA.ClientID,
		sdk.NewInt64Coin(sdk.DefaultBondDenom, 321), a.SenderAccount.GetAddress().String(), b.SenderAccount.GetAddress().String(),
		clienttypes.ZeroHeight(), uint64(coordinator.CurrentTime.Add(time.Hour).Unix()), "", transfertypes.EncodingProtobuf)
	result, err := a.SendMsgs(msg)
	require.NoError(t, err)
	packets, err := ibctesting.ParseIBCV2Packets(channeltypes.EventTypeSendPacket, result.Events)
	require.NoError(t, err)
	require.Len(t, packets, 1)
	require.NoError(t, path.EndpointB.UpdateClient())
	require.NoError(t, path.EndpointA.RelayPacket(packets[0]))
	denom := transfertypes.NewDenom(sdk.DefaultBondDenom, transfertypes.NewHop(transfertypes.PortID, path.EndpointB.ClientID)).IBCDenom()
	require.Equal(t, "321", b.GetSimApp().BankKeeper.GetBalance(b.GetContext(), b.SenderAccount.GetAddress(), denom).Amount.String())
	current, found := a.GetSimApp().RatelimitKeeper.GetRateLimit(a.GetContext(), sdk.DefaultBondDenom, path.EndpointA.ClientID)
	require.True(t, found)
	require.Equal(t, "321", current.Flow.Outflow.String())
	before := a.GetSimApp().BankKeeper.GetBalance(a.GetContext(), a.SenderAccount.GetAddress(), sdk.DefaultBondDenom)
	msg.Token = sdk.NewInt64Coin(sdk.DefaultBondDenom, 2000)
	_, err = a.SendMsgs(msg)
	require.ErrorContains(t, err, "quota exceeded")
	require.Equal(t, before, a.GetSimApp().BankKeeper.GetBalance(a.GetContext(), a.SenderAccount.GetAddress(), sdk.DefaultBondDenom))
}
