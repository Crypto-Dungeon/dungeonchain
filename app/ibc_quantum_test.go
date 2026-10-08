package app

import (
	"bytes"
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/crypto/mldsa65"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	cmttypes "github.com/cometbft/cometbft/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	commitmenttypes "github.com/cosmos/ibc-go/v10/modules/core/23-commitment/types"
	ibctm "github.com/cosmos/ibc-go/v10/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/require"
)

// Exercise Dungeon's registered IBC client route, including protobuf wire
// decoding, rather than only testing the upstream cryptographic primitive.
func TestIBCPostQuantumCounterparty(t *testing.T) {
	pq, err := mldsa65.GenPrivKey()
	require.NoError(t, err)
	for name, key := range map[string]crypto.PrivKey{
		"ed25519":   ed25519.GenPrivKey(),
		"ml_dsa_65": pq,
	} {
		t.Run(name, func(t *testing.T) {
			app := Setup(t)
			now := time.Now().UTC()
			ctx := app.NewContextLegacy(false, tmproto.Header{Height: 2, Time: now})
			validators := cmttypes.NewValidatorSet([]*cmttypes.Validator{
				cmttypes.NewValidator(key.PubKey(), 10),
			})
			trustedHeight := clienttypes.NewHeight(1, 10)
			clientState := ibctm.NewClientState("counterparty-1", ibctm.DefaultTrustLevel,
				24*time.Hour, 48*time.Hour, time.Minute, trustedHeight,
				commitmenttypes.GetSDKSpecs(), []string{"upgrade", "upgradedIBCState"})
			consensusState := ibctm.NewConsensusState(now.Add(-2*time.Minute),
				commitmenttypes.NewMerkleRoot(bytes.Repeat([]byte{1}, 32)), validators.Hash())
			clientID, err := app.IBCKeeper.ClientKeeper.CreateClient(ctx, ibctm.ModuleName,
				app.AppCodec().MustMarshal(clientState), app.AppCodec().MustMarshal(consensusState))
			require.NoError(t, err)

			header := signedCounterpartyHeader(t, key, validators, now.Add(-time.Minute))
			// Round-trip the actual IBC header encoding used by MsgUpdateClient.
			encoded := app.AppCodec().MustMarshal(header)
			var decoded ibctm.Header
			require.NoError(t, app.AppCodec().Unmarshal(encoded, &decoded))
			require.NoError(t, decoded.ValidateBasic())

			bad := signedCounterpartyHeader(t, key, validators, now.Add(-time.Minute))
			bad.Commit.Signatures[0].Signature[0] ^= 0xff
			require.Error(t, app.IBCKeeper.ClientKeeper.UpdateClient(ctx, clientID, bad),
				"a forged commit must never update the client")
			require.NoError(t, app.IBCKeeper.ClientKeeper.UpdateClient(ctx, clientID, &decoded))
			updated, found := app.IBCKeeper.ClientKeeper.GetClientState(ctx, clientID)
			require.True(t, found)
			require.Equal(t, clienttypes.NewHeight(1, 11), updated.(*ibctm.ClientState).LatestHeight)

			// This release verifies counterparties; it does not opt Dungeon into PQ consensus.
			params, err := app.ConsensusParamsKeeper.ParamsStore.Get(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{"ed25519"}, params.Validator.PubKeyTypes)
		})
	}
}

func signedCounterpartyHeader(t *testing.T, key crypto.PrivKey, validators *cmttypes.ValidatorSet, timestamp time.Time) *ibctm.Header {
	t.Helper()
	hash := bytes.Repeat([]byte{1}, 32)
	header := &cmttypes.Header{
		Version: version.Consensus{Block: 11}, ChainID: "counterparty-1", Height: 11, Time: timestamp,
		ValidatorsHash: validators.Hash(), NextValidatorsHash: validators.Hash(),
		ConsensusHash: hash, AppHash: hash, ProposerAddress: key.PubKey().Address(),
	}
	blockID := cmttypes.BlockID{Hash: header.Hash(), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: hash}}
	vote := &cmttypes.Vote{
		Type: tmproto.PrecommitType, Height: 11, Round: 0, BlockID: blockID,
		Timestamp: timestamp, ValidatorAddress: key.PubKey().Address(), ValidatorIndex: 0,
	}
	signature, err := key.Sign(cmttypes.VoteSignBytes(header.ChainID, vote.ToProto()))
	require.NoError(t, err)
	commit := &cmttypes.Commit{Height: 11, Round: 0, BlockID: blockID, Signatures: []cmttypes.CommitSig{{
		BlockIDFlag: cmttypes.BlockIDFlagCommit, ValidatorAddress: key.PubKey().Address(),
		Timestamp: timestamp, Signature: signature,
	}}}
	set, err := validators.ToProto()
	require.NoError(t, err)
	return &ibctm.Header{
		SignedHeader: (&cmttypes.SignedHeader{Header: header, Commit: commit}).ToProto(),
		ValidatorSet: set, TrustedHeight: clienttypes.NewHeight(1, 10), TrustedValidators: set,
	}
}
