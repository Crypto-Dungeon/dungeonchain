package app

// The compatibility port runs upstream tokenfactory tests against Dungeon's
// actual SDK55 application instead of the upstream SDK50/IBC8 sample chain.
import (
	"cosmossdk.io/log/v2"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	dungeonapp "github.com/Crypto-Dungeon/dungeonchain/app"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	tokenfactorytypes "github.com/strangelove-ventures/tokenfactory/x/tokenfactory/types"
	"io"
)

const appName = "tokenfactory-test"

var DefaultNodeHome = dungeonapp.DefaultNodeHome

type TokenFactoryApp struct {
	*dungeonapp.ChainApp
	appCodec     codec.Codec
	configurator module.Configurator
}

func NewApp(logger log.Logger, db dbm.DB, traceStore io.Writer, loadLatest bool,
	appOpts servertypes.AppOptions, wasmOpts []wasmkeeper.Option,
	baseAppOptions ...func(*baseapp.BaseApp)) *TokenFactoryApp {
	chain := dungeonapp.NewChainApp(logger, db, traceStore, loadLatest, appOpts, wasmOpts, baseAppOptions...)
	// Upstream sudo tests explicitly inject an allowed sudoer. Enable only
	// their fixture capability; Dungeon production retains sudo mint disabled.
	capabilities := append([]string(nil), chain.TokenFactoryKeeper.GetEnabledCapabilities()...)
	chain.TokenFactoryKeeper.SetEnabledCapabilities(sdk.Context{}, append(capabilities, tokenfactorytypes.EnableSudoMint))
	return &TokenFactoryApp{ChainApp: chain, appCodec: chain.AppCodec(),
		configurator: module.NewConfigurator(chain.AppCodec(), chain.MsgServiceRouter(), chain.GRPCQueryRouter())}
}
func GetMaccPerms() map[string][]string { return dungeonapp.GetMaccPerms() }
func BlockedAddresses() map[string]bool { return dungeonapp.BlockedAddresses() }
