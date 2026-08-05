package codexcompat_test

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/codexcompat"
	"github.com/stretchr/testify/assert"
)

func TestIsGateCapableOfficialCodex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, ua, originator string
		want                 bool
	}{
		{"cli ua", "codex_cli_rs/0.146.0 (linux; x86_64)", "codex_cli_rs", true},
		{"tui ua", "codex-tui/0.142.0 (Darwin; arm64)", "", true},
		{"originator only no version ua", "curl/8.0", "codex_cli_rs", false},
		{"go ua with originator", "Go-http-client/1.1", "codex_cli_rs", false},
		{"empty", "", "", false},
		// Substring UA is not strict-official; without official originator, gate fails.
		// (Official originator + parseable X.Y.Z would still be gate-capable per the formula.)
		{"evil substring ua", "Mozilla codex_cli_rs/0.1.0", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, codexcompat.IsGateCapableOfficialCodex(tc.ua, tc.originator))
		})
	}
}

func TestParseEngineVersion(t *testing.T) {
	t.Parallel()
	v, ok := codexcompat.ParseEngineVersion("codex_cli_rs/0.146.0 (linux; x86_64)")
	assert.True(t, ok)
	assert.Equal(t, "0.146.0", v)
	_, ok = codexcompat.ParseEngineVersion("curl/8.0")
	assert.False(t, ok)
}
