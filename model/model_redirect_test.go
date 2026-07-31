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
			// Cache stores higher priority first (as buildModelRedirectCacheMap does).
			Targets: []RedirectCandidate{
				{ChannelID: 1, Model: "gpt-4o", Priority: 100},
				{ChannelID: 2, Model: "claude", Priority: 50},
			},
		},
	}
	modelRedirectLoaded = true
	modelRedirectCacheMu.Unlock()
	t.Cleanup(func() {
		modelRedirectCacheMu.Lock()
		modelRedirectCache = nil
		modelRedirectLoaded = false
		modelRedirectCacheMu.Unlock()
	})

	if _, ok := ResolveModelRedirect("ha", "vip"); ok {
		t.Fatal("expected miss for wrong group")
	}
	cands, ok := ResolveModelRedirect("ha", "default")
	if !ok || len(cands) != 2 {
		t.Fatalf("expected 2 candidates, ok=%v len=%d", ok, len(cands))
	}
	// Resolve returns a copy in cache order (priority DESC), without shuffle.
	if cands[0].Priority != 100 || cands[0].Model != "gpt-4o" {
		t.Fatalf("unexpected first candidate: %+v", cands[0])
	}
	if cands[1].Priority != 50 || cands[1].Model != "claude" {
		t.Fatalf("unexpected second candidate: %+v", cands[1])
	}
	// Defensive copy: mutating result must not affect cache.
	cands[0].Model = "mutated"
	cands2, _ := ResolveModelRedirect("ha", "default")
	if cands2[0].Model != "gpt-4o" {
		t.Fatalf("cache was mutated via returned slice: %+v", cands2[0])
	}
}

func TestNormalizeTargetPriority(t *testing.T) {
	if got := normalizeTargetPriority(0, 0); got != modelRedirectDefaultPrio {
		t.Fatalf("omit priority: got %d", got)
	}
	if got := normalizeTargetPriority(0, 200); got != 1 {
		t.Fatalf("omit priority clamp: got %d", got)
	}
	if got := normalizeTargetPriority(modelRedirectMaxPriority+1, 0); got != modelRedirectMaxPriority {
		t.Fatalf("max clamp: got %d", got)
	}
}

func TestOrderRedirectCandidates_SamePriorityLoadBalance(t *testing.T) {
	cands := []RedirectCandidate{
		{ChannelID: 1, Model: "a", Priority: 10},
		{ChannelID: 2, Model: "b", Priority: 10},
		{ChannelID: 3, Model: "c", Priority: 5},
	}
	// Run enough times that both same-priority peers should appear first sometimes.
	firstA, firstB := 0, 0
	for i := 0; i < 80; i++ {
		out := orderRedirectCandidates(cands)
		if len(out) != 3 {
			t.Fatalf("len=%d", len(out))
		}
		if out[2].ChannelID != 3 {
			t.Fatalf("lowest priority should be last: %+v", out)
		}
		switch out[0].ChannelID {
		case 1:
			firstA++
		case 2:
			firstB++
		default:
			t.Fatalf("unexpected first: %+v", out[0])
		}
	}
	if firstA == 0 || firstB == 0 {
		t.Fatalf("expected load balance among same priority, firstA=%d firstB=%d", firstA, firstB)
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
