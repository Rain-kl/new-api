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
			CodexIdentityMode:  "bogus",
			CodexClientVersion: "not-a-version",
		}).ValidateCodexCompat())
		require.NoError(t, (*ChannelSettings)(nil).ValidateCodexCompat())
	})

	t.Run("enabled empty mode is auto and requires version", func(t *testing.T) {
		t.Parallel()
		err := (&ChannelSettings{CodexCompatEnabled: true}).ValidateCodexCompat()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "codex_client_version")

		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexClientVersion: "0.146.0",
		}).ValidateCodexCompat())
	})

	t.Run("valid modes and versions", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexIdentityMode:  CodexIdentityModeAuto,
			CodexClientVersion: "0.146.0",
			CodexClientName:    "codex_cli_rs",
		}).ValidateCodexCompat())
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexIdentityMode:  "AUTO",
			CodexClientVersion: "1.2.3-beta",
		}).ValidateCodexCompat())
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexIdentityMode:  CodexIdentityModeSynthesize,
			CodexClientVersion: "10.0.0",
		}).ValidateCodexCompat())
		// passthrough does not require version
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexIdentityMode:  CodexIdentityModePassthrough,
		}).ValidateCodexCompat())
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexIdentityMode:  "  passthrough  ",
			CodexClientVersion: "garbage",
		}).ValidateCodexCompat())
	})

	t.Run("invalid mode", func(t *testing.T) {
		t.Parallel()
		err := (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexIdentityMode:  "preserve",
			CodexClientVersion: "0.146.0",
		}).ValidateCodexCompat()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "codex_identity_mode")
	})

	t.Run("invalid or empty version for auto and synthesize", func(t *testing.T) {
		t.Parallel()
		for _, mode := range []string{CodexIdentityModeAuto, CodexIdentityModeSynthesize, ""} {
			err := (&ChannelSettings{
				CodexCompatEnabled: true,
				CodexIdentityMode:  mode,
				CodexClientVersion: "",
			}).ValidateCodexCompat()
			require.Error(t, err, "mode=%q", mode)
			assert.Contains(t, err.Error(), "codex_client_version")

			err = (&ChannelSettings{
				CodexCompatEnabled: true,
				CodexIdentityMode:  mode,
				CodexClientVersion: "v1.2.3",
			}).ValidateCodexCompat()
			require.Error(t, err, "mode=%q", mode)
			assert.Contains(t, err.Error(), "codex_client_version")

			err = (&ChannelSettings{
				CodexCompatEnabled: true,
				CodexIdentityMode:  mode,
				CodexClientVersion: "1.2",
			}).ValidateCodexCompat()
			require.Error(t, err, "mode=%q", mode)
			assert.Contains(t, err.Error(), "codex_client_version")
		}
	})

	t.Run("name is optional at validation time", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, (&ChannelSettings{
			CodexCompatEnabled: true,
			CodexIdentityMode:  CodexIdentityModeSynthesize,
			CodexClientVersion: "0.1.0",
			CodexClientName:    "",
		}).ValidateCodexCompat())
		assert.Equal(t, "codex_cli_rs", DefaultCodexClientName)
	})
}

func TestChannelSettingsCodexCompatJSONRoundTrip(t *testing.T) {
	t.Parallel()

	// Zero defaults omit enabled and related fields.
	empty, err := json.Marshal(ChannelSettings{})
	require.NoError(t, err)
	assert.NotContains(t, string(empty), "codex_compat")
	assert.NotContains(t, string(empty), "codex_client")
	assert.NotContains(t, string(empty), "codex_identity")
	assert.NotContains(t, string(empty), "chat_completions_to_responses")

	src := ChannelSettings{
		CodexCompatEnabled:         true,
		CodexClientVersion:         "0.146.0",
		CodexClientName:            "codex_cli_rs",
		CodexIdentityMode:          CodexIdentityModeAuto,
		ChatCompletionsToResponses: true,
	}
	encoded, err := json.Marshal(src)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"codex_compat_enabled":true`)
	assert.Contains(t, string(encoded), `"codex_client_version":"0.146.0"`)
	assert.Contains(t, string(encoded), `"codex_client_name":"codex_cli_rs"`)
	assert.Contains(t, string(encoded), `"codex_identity_mode":"auto"`)
	assert.Contains(t, string(encoded), `"chat_completions_to_responses":true`)

	var decoded ChannelSettings
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, src, decoded)
}
