package globalfee

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"

	sdkmath "cosmossdk.io/math"
	store "github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	appparams "github.com/strangelove-ventures/globalfee/app/params"
	globalfeekeeper "github.com/strangelove-ventures/globalfee/x/globalfee/keeper"
	"github.com/strangelove-ventures/globalfee/x/globalfee/types"
)

func TestDefaultGenesis(t *testing.T) {
	encCfg := appparams.MakeEncodingConfig()
	gotJSON := AppModuleBasic{}.DefaultGenesis(encCfg.Codec)
	assert.JSONEq(t, `{"params":{"minimum_gas_prices":[]}}`, string(gotJSON), string(gotJSON))
}

func TestValidateGenesis(t *testing.T) {
	encCfg := appparams.MakeEncodingConfig()
	specs := map[string]struct {
		src    string
		expErr bool
	}{
		"all good": {
			src: `{"params":{"minimum_gas_prices":[{"denom":"ALX", "amount":"1"}]}}`,
		},
		"empty minimum": {
			src: `{"params":{"minimum_gas_prices":[]}}`,
		},
		"minimum not set": {
			src: `{"params":{}}`,
		},
		"zero amount allowed": {
			src:    `{"params":{"minimum_gas_prices":[{"denom":"ALX", "amount":"0"}]}}`,
			expErr: false,
		},
		"duplicate denoms not allowed": {
			src:    `{"params":{"minimum_gas_prices":[{"denom":"ALX", "amount":"1"},{"denom":"ALX", "amount":"2"}]}}`,
			expErr: true,
		},
		"negative amounts not allowed": {
			src:    `{"params":{"minimum_gas_prices":[{"denom":"ALX", "amount":"-1"}]}}`,
			expErr: true,
		},
		"denom must be sorted": {
			src:    `{"params":{"minimum_gas_prices":[{"denom":"ZLX", "amount":"1"},{"denom":"ALX", "amount":"2"}]}}`,
			expErr: true,
		},
		"sorted denoms is allowed": {
			src:    `{"params":{"minimum_gas_prices":[{"denom":"ALX", "amount":"1"},{"denom":"ZLX", "amount":"2"}]}}`,
			expErr: false,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			gotErr := AppModuleBasic{}.ValidateGenesis(encCfg.Codec, nil, []byte(spec.src))
			if spec.expErr {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
		})
	}
}

func TestInitExportGenesis(t *testing.T) {
	specs := map[string]struct {
		src string
		exp types.GenesisState
	}{
		"single fee": {
			src: `{"params":{"minimum_gas_prices":[{"denom":"ALX", "amount":"1"}]}}`,
			exp: types.GenesisState{Params: types.Params{MinimumGasPrices: sdk.NewDecCoins(sdk.NewDecCoin("ALX", sdkmath.NewInt(1)))}},
		},
		"multiple fee options": {
			src: `{"params":{"minimum_gas_prices":[{"denom":"ALX", "amount":"1"}, {"denom":"BLX", "amount":"0.001"}]}}`,
			exp: types.GenesisState{Params: types.Params{MinimumGasPrices: sdk.NewDecCoins(sdk.NewDecCoin("ALX", sdkmath.NewInt(1)),
				sdk.NewDecCoinFromDec("BLX", sdkmath.LegacyNewDecWithPrec(1, 3)))}},
		},
		"no fee set": {
			src: `{"params":{}}`,
			exp: types.GenesisState{Params: types.Params{MinimumGasPrices: sdk.DecCoins{}}},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			ctx, encCfg, keeper := setupTestStore(t)
			m := NewAppModule(encCfg.Codec, keeper)
			m.InitGenesis(ctx, encCfg.Codec, []byte(spec.src))
			gotJSON := m.ExportGenesis(ctx, encCfg.Codec)
			var got types.GenesisState
			t.Log(got)
			require.NoError(t, encCfg.Codec.UnmarshalJSON(gotJSON, &got))
			assert.Equal(t, spec.exp, got, string(gotJSON))
		})
	}
}

func setupTestStore(t *testing.T) (sdk.Context, appparams.EncodingConfig, globalfeekeeper.Keeper) {
	t.Helper()
	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db, log.NewNopLogger())
	encCfg := appparams.MakeEncodingConfig()
	keyParams := storetypes.NewKVStoreKey(types.StoreKey)
	// globalfeeParams := sdk.NewKVStoreKey(types.StoreKey)
	// tkeyParams := sdk.NewTransientStoreKey(paramstypes.TStoreKey)
	ms.MountStoreWithDB(keyParams, storetypes.StoreTypeIAVL, db)
	// ms.MountStoreWithDB(tkeyParams, storetypes.StoreTypeTransient, db)
	require.NoError(t, ms.LoadLatestVersion())

	globalfeeKeeper := globalfeekeeper.NewKeeper(encCfg.Codec, keyParams, "juno1jv65s3grqf6v6jl3dp4t6c9t9rk99cd83d88wr")

	ctx := sdk.NewContext(ms, tmproto.Header{
		Height:  1234567,
		Time:    time.Date(2020, time.April, 22, 12, 0, 0, 0, time.UTC),
		ChainID: "testing",
	}, false, log.NewNopLogger())

	return ctx, encCfg, globalfeeKeeper
}
