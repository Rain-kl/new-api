package codexcompat

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// IdentityMode controls whether outbound Codex identity is preserved or synthesized.
type IdentityMode string

const (
	IdentityModeAuto        IdentityMode = "auto"
	IdentityModePassthrough IdentityMode = "passthrough"
	IdentityModeSynthesize  IdentityMode = "synthesize"
)

// StickyInput carries inbound identity fields plus the token-scoped seed components
// used when headers/body do not supply conversation keys.
type StickyInput struct {
	ChannelID      int
	UserID         int
	TokenID        int
	SessionID      string
	ThreadID       string
	WindowID       string
	PromptCacheKey string
	UserAgent      string
	Originator     string
}

// StickyIDs is the resolved stable identity used for headers and body prompt_cache_key.
type StickyIDs struct {
	SessionID      string
	ThreadID       string
	WindowID       string
	PromptCacheKey string
}

// Stable UUID5 namespaces (custom constants; not random per process).
var (
	nsThread  = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c1") // NameSpaceURL-like
	nsSession = uuid.MustParse("6ba7b811-9dad-11d1-80b4-00c04fd430c1")
	nsWindow  = uuid.MustParse("6ba7b812-9dad-11d1-80b4-00c04fd430c1")
)

// ResolveStickyIDs merges inbound conversation keys with deterministic token-scoped
// fallbacks. Order for thread:
//  1. inbound thread_id
//  2. body prompt_cache_key
//  3. inbound session_id (as thread seed)
//  4. UUID5 from channel|user|token
//
// prompt_cache_key defaults to the resolved thread id when missing/empty.
// Never uses per-request random UUIDs.
func ResolveStickyIDs(in StickyInput) StickyIDs {
	seed := fmt.Sprintf("%d|%d|%d", in.ChannelID, in.UserID, in.TokenID)

	thread := firstNonEmpty(
		strings.TrimSpace(in.ThreadID),
		strings.TrimSpace(in.PromptCacheKey),
		strings.TrimSpace(in.SessionID),
	)
	if thread == "" {
		thread = uuid.NewSHA1(nsThread, []byte(seed)).String()
	}

	session := strings.TrimSpace(in.SessionID)
	if session == "" {
		session = uuid.NewSHA1(nsSession, []byte(seed)).String()
	}

	window := strings.TrimSpace(in.WindowID)
	if window == "" {
		window = uuid.NewSHA1(nsWindow, []byte(seed)).String()
	}

	pck := strings.TrimSpace(in.PromptCacheKey)
	if pck == "" {
		pck = thread
	}

	return StickyIDs{
		SessionID:      session,
		ThreadID:       thread,
		WindowID:       window,
		PromptCacheKey: pck,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
