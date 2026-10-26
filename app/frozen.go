package app

import (
	"fmt"

	"cosmossdk.io/core/address"
)

// mustDecodeAccounts turns the v13 frozen-account list into raw addresses for
// the ante handler. A bad entry is a build mistake, so it panics at startup.
func mustDecodeAccounts(ac address.Codec, bech []string) [][]byte {
	out := make([][]byte, 0, len(bech))
	for _, b := range bech {
		bz, err := ac.StringToBytes(b)
		if err != nil {
			panic(fmt.Errorf("frozen account %s: %w", b, err))
		}
		out = append(out, bz)
	}
	return out
}
