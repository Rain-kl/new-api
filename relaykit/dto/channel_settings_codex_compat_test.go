package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSettingsValidateCodexCompat(t *testing.T) {
	t.Parallel()

	t.Run("disabled skips validation", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, (&ChannelSettings{}).ValidateCodexCompat())
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: false,
			CodexClientVersion: "not-a-version",
		}).ValidateCodexCompat())
		require.NoError(t, (*ChannelSettings)(nil).ValidateCodexCompat())
	})

	t.Run("enabled requires version", func(t *testing.T) {
		t.Parallel()
		err := (&ChannelSettings{CodexCompatEnabled: true}).ValidateCodexCompat()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "codex_client_version")
	})

	t.Run("valid versions pass", func(t *testing.T) {
		t.Parallel()
		for _, version := range []string{"0.146.0", "1.2.3-beta", "10.0.0"} {
			require.NoError(t, (&ChannelSettings{
				CodexCompatEnabled: true,
				CodexClientVersion: version,
			}).ValidateCodexCompat(), "version=%q", version)
		}
	})

	t.Run("invalid versions fail", func(t *testing.T) {
		t.Parallel()
		// ValidateCodexCompat trims whitespace before matching, so a padded
		// valid version (e.g. " 0.146.0 ") is accepted; only genuinely invalid
		// strings fail.
		for _, version := range []string{"v1.2.3", "1.2", "abc"} {
			err := (&ChannelSettings{
				CodexCompatEnabled: true,
				CodexClientVersion: version,
			}).ValidateCodexCompat()
			require.Error(t, err, "version=%q", version)
			assert.Contains(t, err.Error(), "codex_client_version")
		}
	})

	t.Run("name is optional at validation time", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexClientVersion: "0.1.0",
		}).ValidateCodexCompat())
		assert.Equal(t, "codex_cli_rs", DefaultCodexClientName)
	})
}

func TestChannelSettingsCodexCompatJSONRoundTrip(t *testing.T) {
	t.Parallel()

	empty, err := json.Marshal(ChannelSettings{})
	require.NoError(t, err)
	assert.NotContains(t, string(empty), "codex_compat")
	assert.NotContains(t, string(empty), "codex_client")
	assert.NotContains(t, string(empty), "codex_identity")
	assert.NotContains(t, string(empty), "chat_completions_to_responses")

	src := ChannelSettings{
		CodexCompatEnabled: true,
		CodexClientVersion: "0.146.0",
		CodexClientName:    "codex_cli_rs",
	}
	encoded, err := json.Marshal(src)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"codex_compat_enabled":true`)
	assert.Contains(t, string(encoded), `"codex_client_version":"0.146.0"`)
	assert.Contains(t, string(encoded), `"codex_client_name":"codex_cli_rs"`)
	assert.NotContains(t, string(encoded), "codex_identity")
	assert.NotContains(t, string(encoded), "chat_completions_to_responses")

	var decoded ChannelSettings
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, src, decoded)
}
