package e2e

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	interchaintestrelayer "github.com/strangelove-ventures/interchaintest/v8/relayer"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

// TestIBCClientRouting is a regression test for the v7.0.0 IBC outage.
//
// v7.0.0 bumped ibc-go v8 -> v10 but never registered the light-client modules
// with the 02-client router (clientKeeper.AddRoute(ibctm.ModuleName, ...)). As a
// result ClientKeeper.GetClientStatus() returned "Unauthorized" for every
// tendermint client (route not found) and ALL IBC packet sends failed chain-wide,
// while the chain itself kept producing blocks. A single-chain upgrade test cannot
// catch this because it has no counterparty and no relayer.
//
// This test stands up TWO dungeonchain instances, opens a transfer channel,
// asserts the client is Active, and round-trips a token both directions. On a
// binary missing the AddRoute wiring it fails at ic.Build (the connection
// handshake cannot verify proofs through the unregistered light client) or at the
// explicit Active-status assertion below.
//
// Requires the chain image built as dungeonchain:local (see `make local-image`).
func TestIBCClientRouting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rep := testreporter.NewNopReporter()
	eRep := rep.RelayerExecReporter(t)
	client, network := interchaintest.DockerSetup(t)

	// Copy the package-level specs into locals so a parallel test cannot race on
	// the shared vars (interchaintest may mutate the spec it is handed).
	chainASpec := DefaultChainSpec
	chainBSpec := SecondDefaultChainSpec

	cf := interchaintest.NewBuiltinChainFactory(zaptest.NewLogger(t), []*interchaintest.ChainSpec{
		&chainASpec,
		&chainBSpec,
	})

	chains, err := cf.Chains(t.Name())
	require.NoError(t, err)
	chainA, chainB := chains[0].(*cosmos.CosmosChain), chains[1].(*cosmos.CosmosChain)

	r := interchaintest.NewBuiltinRelayerFactory(
		ibc.CosmosRly,
		zaptest.NewLogger(t, zaptest.Level(zapcore.InfoLevel)),
		interchaintestrelayer.CustomDockerImage(RelayerRepo, RelayerVersion, "100:1000"),
		interchaintestrelayer.StartupFlags("--processor", "events", "--block-history", "100"),
	).Build(t, client, network)

	const ibcPath = "dungeon-dungeon"

	ic := interchaintest.NewInterchain().
		AddChain(chainA).
		AddChain(chainB).
		AddRelayer(r, "relayer").
		AddLink(interchaintest.InterchainLink{
			Chain1:  chainA,
			Chain2:  chainB,
			Relayer: r,
			Path:    ibcPath,
		})

	// Build creates the clients, the connection handshake, and the transfer
	// channel. This step alone fails on the broken binary: ConnOpenTry/Ack must
	// verify proofs through the tendermint light client, which is unrouted.
	require.NoError(t, ic.Build(ctx, eRep, interchaintest.InterchainBuildOptions{
		TestName:         t.Name(),
		Client:           client,
		NetworkID:        network,
		SkipPathCreation: false,
	}))
	t.Cleanup(func() { _ = ic.Close() })

	// Explicit guard that names the regression: the client backing the connection
	// must be Active. On the broken binary this reports "Unauthorized".
	var status struct {
		Status string `json:"status"`
	}
	ExecuteQuery(ctx, chainA, []string{"query", "ibc", "client", "status", "07-tendermint-0"}, &status)
	require.Equal(t, "Active", status.Status,
		"tendermint client must be Active; Unauthorized means the 02-client router is missing AddRoute(ibctm.ModuleName)")

	// Fund users on each chain.
	fundAmount := math.NewInt(10_000_000)
	users := interchaintest.GetAndFundTestUsers(t, ctx, "default", fundAmount, chainA, chainB)
	userA, userB := users[0], users[1]

	aChannels, err := r.GetChannels(ctx, eRep, chainA.Config().ChainID)
	require.NoError(t, err)
	aChannelID, err := getTransferChannel(aChannels)
	require.NoError(t, err)

	bChannels, err := r.GetChannels(ctx, eRep, chainB.Config().ChainID)
	require.NoError(t, err)
	bChannelID, err := getTransferChannel(bChannels)
	require.NoError(t, err)

	// --- A -> B: proves MsgSendPacket + RecvPacket route through the LCM ---
	send := math.NewInt(1_000_000)
	_, err = chainA.SendIBCTransfer(ctx, aChannelID, userA.KeyName(), ibc.WalletAmount{
		Address: userB.FormattedAddress(),
		Denom:   chainA.Config().Denom,
		Amount:  send,
	}, ibc.TransferOptions{})
	require.NoError(t, err)
	require.NoError(t, r.Flush(ctx, eRep, ibcPath, aChannelID))

	aBal, err := chainA.GetBalance(ctx, userA.FormattedAddress(), chainA.Config().Denom)
	require.NoError(t, err)
	require.True(t, aBal.Equal(fundAmount.Sub(send)), "sender's native balance should decrease by the sent amount")

	dstDenom := transfertypes.ParseDenomTrace(
		transfertypes.GetPrefixedDenom("transfer", bChannelID, chainA.Config().Denom),
	).IBCDenom()
	bBal, err := chainB.GetBalance(ctx, userB.FormattedAddress(), dstDenom)
	require.NoError(t, err)
	require.True(t, bBal.Equal(send), "receiver should hold the IBC voucher (RecvPacket succeeded)")

	// --- B -> A: send the voucher back; proves the timeout/ack + unescrow path ---
	_, err = chainB.SendIBCTransfer(ctx, bChannelID, userB.KeyName(), ibc.WalletAmount{
		Address: userA.FormattedAddress(),
		Denom:   dstDenom,
		Amount:  send,
	}, ibc.TransferOptions{})
	require.NoError(t, err)
	require.NoError(t, r.Flush(ctx, eRep, ibcPath, bChannelID))

	aBalFinal, err := chainA.GetBalance(ctx, userA.FormattedAddress(), chainA.Config().Denom)
	require.NoError(t, err)
	require.True(t, aBalFinal.Equal(fundAmount), "round-trip should restore the sender's native balance")
}
