package v13roles

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
	address "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/config-*.json
var fixtures embed.FS

func values(t *testing.T, target Target) (string, string) {
	t.Helper()
	if !target.Canonical {
		return v13.CompromisedHotWallet, v13.RecoveryAddress
	}
	codec := address.NewBech32Codec("dungeon")
	old, err := codec.StringToBytes(v13.CompromisedHotWallet)
	require.NoError(t, err)
	clean, err := codec.StringToBytes(v13.RecoveryAddress)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(old), base64.StdEncoding.EncodeToString(clean)
}

func TestPinnedRoleReplacement(t *testing.T) {
	for i, target := range Targets {
		t.Run(fmt.Sprint(target.CodeID), func(t *testing.T) {
			raw, err := fixtures.ReadFile(fmt.Sprintf("testdata/config-%d.json", i))
			require.NoError(t, err)
			old, clean := values(t, target)
			after, err := replaceRole(raw, target.Field, old, clean, target.ConfigHash)
			require.NoError(t, err)
			var beforeJSON, afterJSON map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &beforeJSON))
			require.NoError(t, json.Unmarshal(after, &afterJSON))
			require.Equal(t, len(beforeJSON), len(afterJSON))
			for field, value := range beforeJSON {
				if field == target.Field {
					var got string
					require.NoError(t, json.Unmarshal(afterJSON[field], &got))
					require.Equal(t, clean, got)
				} else {
					require.True(t, bytes.Equal(value, afterJSON[field]), field)
				}
			}
			rerun, err := replaceRole(after, target.Field, old, clean, target.ConfigHash)
			require.NoError(t, err)
			require.Equal(t, after, rerun)
			changed := bytes.Replace(raw, []byte("config-never-matches"), []byte("x"), 1)
			// Add one unrelated byte of JSON whitespace: semantic equivalence must
			// still fail because this recovery is pinned to the exact source bytes.
			changed = append([]byte(" "), changed...)
			_, err = replaceRole(changed, target.Field, old, clean, target.ConfigHash)
			require.ErrorContains(t, err, "pinned state")
			_, err = replaceRole(raw, target.Field, "unexpected-old", clean, target.ConfigHash)
			require.Error(t, err)
			_, err = replaceRole(raw, "missing", old, clean, target.ConfigHash)
			require.Error(t, err)
		})
	}
}

func TestRecoveryManifestScope(t *testing.T) {
	require.Len(t, Targets, 5)
	seen := map[string]bool{}
	immutable := 0
	for _, target := range Targets {
		require.False(t, seen[target.Address])
		seen[target.Address] = true
		require.Len(t, target.CodeHash, 64)
		require.Len(t, target.ConfigHash, 64)
		if target.MigrationAdmin == "" {
			immutable++
			require.EqualValues(t, 74, target.CodeID)
		} else {
			require.Equal(t, v13.RecoveryAddress, target.MigrationAdmin)
		}
	}
	require.Equal(t, 1, immutable, "immutable Fantasy must remain immutable")
	require.EqualValues(t, v13.ForkHeight+1, ForkHeight)
}
