package main

import (
	"os"

	"cosmossdk.io/log"

	"github.com/Crypto-Dungeon/dungeonchain/app"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
)

func main() {
	rootCmd := NewRootCmd()
	rootCmd.AddCommand(AddConsumerSectionCmd(app.DefaultNodeHome))
	rootCmd.AddCommand(AddFastGenesisAccountCmd(app.DefaultNodeHome, addresscodec.NewBech32Codec(app.Bech32PrefixAccAddr)))

	if err := svrcmd.Execute(rootCmd, "", app.DefaultNodeHome); err != nil {
		log.NewLogger(rootCmd.OutOrStderr()).Error("failure when running app", "err", err)
		os.Exit(1)
	}
}
