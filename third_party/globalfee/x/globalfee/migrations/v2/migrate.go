package v2

import (
	"fmt"

	sdkmath "cosmossdk.io/math"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/strangelove-ventures/globalfee/x/globalfee/types"
)

const (
	ModuleName = "globalfee"
)

var ParamsKey = []byte{0x00}

// Migrate migrates the x/globalfee module state from the consensus version 1 to
// version 2. Specifically, it takes the parameters that are currently stored
// and managed by the x/params modules and stores them directly into the x/globalfee
// module state.
func Migrate(
	_ sdk.Context,
	store storetypes.KVStore,
	cdc codec.BinaryCodec,
	bondDenom string,
) error {
	var currParams types.Params

	if bondDenom == "ujunox" {
		// testnet
		// https://uni-api.reece.sh/gaia/globalfee/v1beta1/minimum_gas_prices
		currParams = types.Params{
			MinimumGasPrices: sdk.DecCoins{
				// 0.003000000000000000uatom
				sdk.NewDecCoinFromDec("ibc/C4CFF46FD6DE35CA4CF4CE031E643C8FDC9BA4B99AE598E9B0ED98FE3A2319F9", sdkmath.LegacyNewDecWithPrec(1, 3)),
				// 0.002500000000000000 ujunox
				sdk.NewDecCoinFromDec(bondDenom, sdkmath.LegacyNewDecWithPrec(25, 4)),
			}.Sort(),
		}
	} else {
		// mainnet
		// https://juno-api.reece.sh/gaia/globalfee/v1beta1/minimum_gas_prices
		currParams = types.Params{
			MinimumGasPrices: sdk.DecCoins{
				// 0.003000000000000000 uatom
				sdk.NewDecCoinFromDec("ibc/C4CFF46FD6DE35CA4CF4CE031E643C8FDC9BA4B99AE598E9B0ED98FE3A2319F9", sdkmath.LegacyNewDecWithPrec(3, 3)),
				// 0.075000000000000000 ujuno
				sdk.NewDecCoinFromDec(bondDenom, sdkmath.LegacyNewDecWithPrec(75, 3)),
			}.Sort(),
		}
	}

	fmt.Printf("migrating %s params: %+v\n", ModuleName, currParams)

	if err := currParams.Validate(); err != nil {
		return err
	}
	bz := cdc.MustMarshal(&currParams)
	store.Set(ParamsKey, bz)

	return nil
}
