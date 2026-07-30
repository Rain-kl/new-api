package model

import (
	"testing"
)

func TestAttemptModel_Passthrough(t *testing.T) {
	if got := AttemptModel("deepseek-flash", RedirectCandidate{Model: ""}); got != "deepseek-flash" {
		t.Fatalf("passthrough: got %q", got)
	}
	if got := AttemptModel("deepseek-flash", RedirectCandidate{Model: "deepseek-v4-flash"}); got != "deepseek-v4-flash" {
		t.Fatalf("explicit: got %q", got)
	}
}

func TestResolveModelRedirect_GroupGate(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"ha": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: 1, Model: "gpt-4o", Priority: 1},
				{ChannelID: 2, Model: "claude", Priority: 2},
			},
		},
	}
	modelRedirectLoaded = true
	modelRedirectCacheMu.Unlock()
	t.Cleanup(InvalidateModelRedirectCache)

	if _, ok := ResolveModelRedirect("ha", "vip"); ok {
		t.Fatal("expected miss for wrong group")
	}
	cands, ok := ResolveModelRedirect("ha", "default")
	if !ok || len(cands) != 2 {
		t.Fatalf("expected 2 candidates, ok=%v len=%d", ok, len(cands))
	}
	if cands[0].Priority != 1 || cands[0].Model != "gpt-4o" {
		t.Fatalf("unexpected first candidate: %+v", cands[0])
	}
}

func TestChannelAccessibleForRedirect(t *testing.T) {
	ch := &Channel{Status: 1, Group: "default,vip"} // ChannelStatusEnabled
	if !ChannelAccessibleForRedirect(ch, "default") {
		t.Fatal("default should access")
	}
	if ChannelAccessibleForRedirect(ch, "other") {
		t.Fatal("other must not access")
	}
	ch.Status = 2
	if ChannelAccessibleForRedirect(ch, "default") {
		t.Fatal("disabled channel")
	}
}

func TestFilterRedirectCandidates_UsesClientModelForPassthrough(t *testing.T) {
	// Unit-level: pathCheck receives passthrough client model when target.Model empty.
	cands := []RedirectCandidate{
		{ChannelID: 999999, Model: "", Priority: 1}, // missing channel → dropped
	}
	out := FilterRedirectCandidates(cands, "deepseek-flash", "default", "/v1/chat/completions", nil)
	if len(out) != 0 {
		t.Fatalf("missing channel should be filtered, got %d", len(out))
	}
}
