package types

import (
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	paramslegacy "github.com/cosmos/cosmos-sdk/x/params/legacy"
)

// Deprecated.
func ConsensusParamsKeyTable() KeyTable {
	return NewKeyTable(
		NewParamSetPair(
			paramslegacy.ParamStoreKeyBlockParams, cmtproto.BlockParams{}, paramslegacy.ValidateBlockParams,
		),
		NewParamSetPair(
			paramslegacy.ParamStoreKeyEvidenceParams, cmtproto.EvidenceParams{}, paramslegacy.ValidateEvidenceParams,
		),
		NewParamSetPair(
			paramslegacy.ParamStoreKeyValidatorParams, cmtproto.ValidatorParams{}, paramslegacy.ValidateValidatorParams,
		),
	)
}
