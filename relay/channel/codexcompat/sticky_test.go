package codexcompat_test

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/codexcompat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveStickyIDs_SameSeedStable(t *testing.T) {
	t.Parallel()
	in := codexcompat.StickyInput{ChannelID: 10, UserID: 20, TokenID: 30}
	a := codexcompat.ResolveStickyIDs(in)
	b := codexcompat.ResolveStickyIDs(in)

	assert.Equal(t, a, b)
	assert.NotEmpty(t, a.SessionID)
	assert.NotEmpty(t, a.ThreadID)
	assert.NotEmpty(t, a.WindowID)
	assert.Equal(t, a.ThreadID, a.PromptCacheKey)
	// Different role seeds must not collide.
	other := codexcompat.ResolveStickyIDs(codexcompat.StickyInput{ChannelID: 10, UserID: 20, TokenID: 31})
	assert.NotEqual(t, a.ThreadID, other.ThreadID)
	assert.NotEqual(t, a.SessionID, other.SessionID)
	assert.NotEqual(t, a.WindowID, other.WindowID)
}

func TestResolveStickyIDs_PromptCacheKeyWinsAsThreadAndCacheKey(t *testing.T) {
	t.Parallel()
	in := codexcompat.StickyInput{
		ChannelID:      1,
		UserID:         2,
		TokenID:        3,
		PromptCacheKey: "body-cache-key-xyz",
	}
	out := codexcompat.ResolveStickyIDs(in)
	assert.Equal(t, "body-cache-key-xyz", out.ThreadID)
	assert.Equal(t, "body-cache-key-xyz", out.PromptCacheKey)
	// Session/window still derived from seed when not provided inbound.
	assert.NotEmpty(t, out.SessionID)
	assert.NotEmpty(t, out.WindowID)
	assert.NotEqual(t, out.SessionID, out.ThreadID)
}

func TestResolveStickyIDs_InboundThreadWinsOverTokenSeed(t *testing.T) {
	t.Parallel()
	in := codexcompat.StickyInput{
		ChannelID: 1,
		UserID:    2,
		TokenID:   3,
		ThreadID:  "client-thread-abc",
	}
	out := codexcompat.ResolveStickyIDs(in)
	assert.Equal(t, "client-thread-abc", out.ThreadID)
	assert.Equal(t, "client-thread-abc", out.PromptCacheKey)

	seedOnly := codexcompat.ResolveStickyIDs(codexcompat.StickyInput{ChannelID: 1, UserID: 2, TokenID: 3})
	assert.NotEqual(t, seedOnly.ThreadID, out.ThreadID)
}

func TestResolveStickyIDs_SessionUsedAsThreadSeedWhenNoThreadOrCacheKey(t *testing.T) {
	t.Parallel()
	in := codexcompat.StickyInput{
		ChannelID: 1,
		UserID:    2,
		TokenID:   3,
		SessionID: "client-session-1",
	}
	out := codexcompat.ResolveStickyIDs(in)
	assert.Equal(t, "client-session-1", out.SessionID)
	assert.Equal(t, "client-session-1", out.ThreadID)
	assert.Equal(t, "client-session-1", out.PromptCacheKey)
}

func TestResolveStickyIDs_PreservesInboundWindowAndSession(t *testing.T) {
	t.Parallel()
	in := codexcompat.StickyInput{
		ChannelID:      1,
		UserID:         2,
		TokenID:        3,
		SessionID:      "sess-in",
		ThreadID:       "thread-in",
		WindowID:       "win-in",
		PromptCacheKey: "cache-in",
	}
	out := codexcompat.ResolveStickyIDs(in)
	require.Equal(t, codexcompat.StickyIDs{
		SessionID:      "sess-in",
		ThreadID:       "thread-in",
		WindowID:       "win-in",
		PromptCacheKey: "cache-in",
	}, out)
}
