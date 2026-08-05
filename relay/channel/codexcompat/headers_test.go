package codexcompat_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/codexcompat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSyntheticUserAgent(t *testing.T) {
	t.Parallel()
	ua := codexcompat.BuildSyntheticUserAgent("codex_cli_rs", "0.146.0")
	assert.Equal(t, "codex_cli_rs/0.146.0 (linux; x86_64)", ua)
	v, ok := codexcompat.ParseEngineVersion(ua)
	require.True(t, ok)
	assert.Equal(t, "0.146.0", v)
}

func TestHasInboundCodexFingerprint(t *testing.T) {
	t.Parallel()
	assert.False(t, codexcompat.HasInboundCodexFingerprint(http.Header{}))
	assert.False(t, codexcompat.HasInboundCodexFingerprint(http.Header{
		"Session-Id": []string{"s1"},
		"User-Agent": []string{"codex_cli_rs/0.1.0"},
	}))
	assert.True(t, codexcompat.HasInboundCodexFingerprint(http.Header{
		"X-Codex-Window-Id": []string{"w1"},
	}))
	assert.True(t, codexcompat.HasInboundCodexFingerprint(http.Header{
		"x-codex-turn-state": []string{"active"},
	}))
}

func TestApplyIdentityHeaders_Synthesize(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{
		SessionID:      "sess-s",
		ThreadID:       "thread-s",
		WindowID:       "win-s",
		PromptCacheKey: "thread-s",
	}
	dst := make(http.Header)
	codexcompat.ApplyIdentityHeaders(&dst, codexcompat.ApplyInput{
		Mode:          codexcompat.IdentityModeSynthesize,
		ClientVersion: "0.146.0",
		ClientName:    "codex_cli_rs",
		Sticky:        sticky,
		Inbound: http.Header{
			"User-Agent": []string{"curl/8.0"},
			"Originator": []string{"curl"},
			"Session-Id": []string{"ignore-me"},
		},
		GateCapable: false,
	})

	assert.Equal(t, "codex_cli_rs/0.146.0 (linux; x86_64)", dst.Get("User-Agent"))
	assert.Equal(t, "codex_cli_rs", dst.Get("originator"))
	assert.Equal(t, "sess-s", dst.Get("session_id"))
	assert.Equal(t, "thread-s", dst.Get("thread_id"))
	assert.Equal(t, "win-s", dst.Get("x-codex-window-id"))
}

func TestApplyIdentityHeaders_AutoSynthesizeWhenNotGateCapable(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{
		SessionID: "sess-a",
		ThreadID:  "thread-a",
		WindowID:  "win-a",
	}
	dst := make(http.Header)
	codexcompat.ApplyIdentityHeaders(&dst, codexcompat.ApplyInput{
		Mode:          codexcompat.IdentityModeAuto,
		ClientVersion: "0.142.0",
		ClientName:    "codex_cli_rs",
		Sticky:        sticky,
		Inbound:       http.Header{"User-Agent": []string{"Go-http-client/1.1"}},
		GateCapable:   false,
	})
	assert.True(t, strings.HasPrefix(dst.Get("User-Agent"), "codex_cli_rs/0.142.0"))
	assert.Equal(t, "sess-a", dst.Get("session_id"))
	assert.Equal(t, "thread-a", dst.Get("thread_id"))
	assert.Equal(t, "win-a", dst.Get("x-codex-window-id"))
}

func TestApplyIdentityHeaders_PreserveGateCapableKeepsSession(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{
		SessionID: "sticky-sess",
		ThreadID:  "sticky-thread",
		WindowID:  "sticky-win",
	}
	inbound := http.Header{}
	inbound.Set("User-Agent", "codex_cli_rs/0.146.0 (linux; x86_64)")
	inbound.Set("originator", "codex_cli_rs")
	inbound.Set("session_id", "client-session")
	inbound.Set("thread_id", "client-thread")
	// Has x-codex fingerprint → do not inject sticky window.
	inbound.Set("x-codex-window-id", "client-window")
	inbound.Set("x-codex-turn-state", "active")

	dst := make(http.Header)
	codexcompat.ApplyIdentityHeaders(&dst, codexcompat.ApplyInput{
		Mode:          codexcompat.IdentityModeAuto,
		ClientVersion: "0.146.0",
		ClientName:    "codex_cli_rs",
		Sticky:        sticky,
		Inbound:       inbound,
		GateCapable:   true,
	})

	assert.Equal(t, "codex_cli_rs/0.146.0 (linux; x86_64)", dst.Get("User-Agent"))
	assert.Equal(t, "codex_cli_rs", dst.Get("originator"))
	assert.Equal(t, "client-session", dst.Get("session_id"))
	assert.Equal(t, "client-thread", dst.Get("thread_id"))
	assert.Equal(t, "client-window", dst.Get("x-codex-window-id"))
	assert.Equal(t, "active", dst.Get("x-codex-turn-state"))
	// Must not overwrite with sticky when fingerprint present.
	assert.NotEqual(t, "sticky-sess", dst.Get("session_id"))
	assert.NotEqual(t, "sticky-win", dst.Get("x-codex-window-id"))
}

func TestApplyIdentityHeaders_PreserveInjectsWindowOnlyWhenNoCodexHeaders(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{
		SessionID: "sticky-sess",
		ThreadID:  "sticky-thread",
		WindowID:  "sticky-win",
	}
	inbound := http.Header{}
	inbound.Set("User-Agent", "codex_cli_rs/0.146.0 (linux; x86_64)")
	inbound.Set("originator", "codex_cli_rs")
	inbound.Set("session_id", "client-session")
	inbound.Set("thread_id", "client-thread")
	// No x-codex-* headers.

	dst := make(http.Header)
	codexcompat.ApplyIdentityHeaders(&dst, codexcompat.ApplyInput{
		Mode:          codexcompat.IdentityModeAuto,
		ClientVersion: "0.146.0",
		ClientName:    "codex_cli_rs",
		Sticky:        sticky,
		Inbound:       inbound,
		GateCapable:   true,
	})

	assert.Equal(t, "client-session", dst.Get("session_id"))
	assert.Equal(t, "client-thread", dst.Get("thread_id"))
	assert.Equal(t, "sticky-win", dst.Get("x-codex-window-id"))
	// Still client UA/originator (preserve).
	assert.Equal(t, "codex_cli_rs/0.146.0 (linux; x86_64)", dst.Get("User-Agent"))
	assert.Equal(t, "codex_cli_rs", dst.Get("originator"))
}

func TestApplyIdentityHeaders_PassthroughUsesPreservePath(t *testing.T) {
	t.Parallel()
	sticky := codexcompat.StickyIDs{WindowID: "sticky-win"}
	inbound := http.Header{}
	inbound.Set("User-Agent", "curl/8.0")
	inbound.Set("session_id", "s1")

	dst := make(http.Header)
	codexcompat.ApplyIdentityHeaders(&dst, codexcompat.ApplyInput{
		Mode:          codexcompat.IdentityModePassthrough,
		ClientVersion: "0.146.0",
		ClientName:    "codex_cli_rs",
		Sticky:        sticky,
		Inbound:       inbound,
		GateCapable:   false, // still preserve path for passthrough
	})
	assert.Equal(t, "curl/8.0", dst.Get("User-Agent"))
	assert.Equal(t, "s1", dst.Get("session_id"))
	assert.Equal(t, "sticky-win", dst.Get("x-codex-window-id"))
}
