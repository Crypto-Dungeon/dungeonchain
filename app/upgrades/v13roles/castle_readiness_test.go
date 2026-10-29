package v13roles

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCastle228ReadinessPins(t *testing.T) {
	target := Targets[3]
	require.EqualValues(t, 228, target.CodeID)
	require.Equal(t, "1fc6bdc8c50b25f62f2520e069341df41948d6d8ccc319f20566edeeef08d57c", target.CodeHash)
	require.Equal(t, "4d1f974138681d730c41cf01ff850f0c5fd6369365ccb49ab07d09ac6a640e1f", target.ConfigHash)
	raw, err := fixtures.ReadFile("testdata/config-3.json")
	require.NoError(t, err)
	old, clean := values(t, target)
	after, err := replaceRole(raw, target.Field, old, clean, target.ConfigHash)
	require.NoError(t, err)
	var beforeConfig, afterConfig map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &beforeConfig))
	require.NoError(t, json.Unmarshal(after, &afterConfig))
	require.JSONEq(t, "3", string(afterConfig["reward_split_from_season"]))
	for field, value := range beforeConfig {
		if field != target.Field {
			require.Equal(t, value, afterConfig[field], field)
		}
	}
	legacy, err := fixtures.ReadFile("testdata/config-castle-v220.json")
	require.NoError(t, err)
	_, err = replaceRole(legacy, target.Field, old, clean, target.ConfigHash)
	require.ErrorContains(t, err, "pinned state")
}
