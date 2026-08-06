package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveModelRedirect_ExpandsNestedBlackBox(t *testing.T) {
	// Parent P: nested→child (P200), real ch=9 model=m9 (P100)
	// Child C: real ch=1 m1 (P50), real ch=2 m2 (P40)
	// Expected order for group default: ch1, ch2, ch9
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"child": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: 1, Model: "m1", Priority: 50},
				{ChannelID: 2, Model: "m2", Priority: 40},
			},
		},
		"parent": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "child", Priority: 200},
				{ChannelID: 9, Model: "m9", Priority: 100},
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

	cands, ok := ResolveModelRedirect("parent", "default")
	require.True(t, ok)
	require.Len(t, cands, 3)
	assert.Equal(t, 1, cands[0].ChannelID)
	assert.Equal(t, "m1", cands[0].Model)
	assert.Equal(t, 2, cands[1].ChannelID)
	assert.Equal(t, 9, cands[2].ChannelID)
	for _, c := range cands {
		require.False(t, c.IsNestedRedirect(), "resolve must not return sentinels")
	}
}

func TestResolveModelRedirect_NestedPassthroughMaterializesChildName(t *testing.T) {
	// child hop: real channel, empty model (passthrough)
	// parent nests child; resolve parent must yield Model=child name not empty
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"child": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: 1, Model: "", Priority: 50},
			},
		},
		"parent": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "child", Priority: 200},
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

	cands, ok := ResolveModelRedirect("parent", "default")
	require.True(t, ok)
	require.Len(t, cands, 1)
	assert.Equal(t, 1, cands[0].ChannelID)
	assert.Equal(t, "child", cands[0].Model)

	// leaf direct request still materializes own name (equiv to passthrough client)
	cands2, ok2 := ResolveModelRedirect("child", "default")
	require.True(t, ok2)
	require.Len(t, cands2, 1)
	assert.Equal(t, "child", cands2[0].Model)
}

func TestResolveModelRedirect_NestedChildGroupMismatchSkipsBranch(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"child": {
			Groups: map[string]struct{}{"vip": {}},
			Targets: []RedirectCandidate{
				{ChannelID: 1, Model: "m1", Priority: 50},
			},
		},
		"parent": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "child", Priority: 200},
				{ChannelID: 9, Model: "m9", Priority: 100},
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

	cands, ok := ResolveModelRedirect("parent", "default")
	require.True(t, ok)
	require.Len(t, cands, 1)
	assert.Equal(t, 9, cands[0].ChannelID)
}

func TestResolveModelRedirect_RuntimeCycleDoesNotPanic(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"a": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "b", Priority: 1},
			},
		},
		"b": {
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "a", Priority: 1},
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

	_, ok := ResolveModelRedirect("a", "default")
	require.False(t, ok)
}

func TestRedirectCandidate_IsNestedRedirect(t *testing.T) {
	require.True(t, RedirectCandidate{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "x"}.IsNestedRedirect())
	require.False(t, RedirectCandidate{ChannelID: 1, Model: "x"}.IsNestedRedirect())
}

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

func TestOrderRedirectCandidates_PreservesExpandOrder(t *testing.T) {
	// Black-box expand order: child hops first (lower original prio), then parent hop (higher prio).
	cands := []RedirectCandidate{
		{ChannelID: 1, Model: "m1", Priority: 50},
		{ChannelID: 2, Model: "m2", Priority: 40},
		{ChannelID: 9, Model: "m9", Priority: 100},
	}
	out := orderRedirectCandidates(cands)
	require.Len(t, out, 3)
	assert.Equal(t, 1, out[0].ChannelID)
	assert.Equal(t, 2, out[1].ChannelID)
	assert.Equal(t, 9, out[2].ChannelID)
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

func TestDetectModelRedirectCycle(t *testing.T) {
	edges := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	require.True(t, detectModelRedirectCycle("a", edges))
	require.False(t, detectModelRedirectCycle("a", map[string][]string{"a": {"b"}, "b": {}}))
	// Self-loop
	require.True(t, detectModelRedirectCycle("a", map[string][]string{"a": {"a"}}))
	// Longer cycle a→b→c→a
	require.True(t, detectModelRedirectCycle("a", map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	}))
	// Diamond without cycle
	require.False(t, detectModelRedirectCycle("a", map[string][]string{
		"a": {"b", "c"},
		"b": {"d"},
		"c": {"d"},
		"d": {},
	}))
}

func setupModelRedirectTestDB(t *testing.T) {
	t.Helper()
	require.NotNil(t, DB, "package TestMain must set model.DB")
	require.NoError(t, DB.AutoMigrate(&ModelRedirect{}, &ModelRedirectTarget{}, &Channel{}))
	require.NoError(t, DB.Where("1 = 1").Delete(&ModelRedirectTarget{}).Error)
	require.NoError(t, DB.Where("1 = 1").Delete(&ModelRedirect{}).Error)
	t.Cleanup(func() {
		_ = DB.Where("1 = 1").Delete(&ModelRedirectTarget{}).Error
		_ = DB.Where("1 = 1").Delete(&ModelRedirect{}).Error
	})
}

func insertEnabledChildRedirect(t *testing.T, name string) {
	t.Helper()
	now := common.GetTimestamp()
	row := &ModelRedirect{
		Name:      name,
		Groups:    "default",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
		Targets: []ModelRedirectTarget{
			// Dummy real hop; validation only Counts by name+enabled for nested existence.
			{ChannelId: 1, Model: "m1", Priority: 100, Enabled: true},
		},
	}
	require.NoError(t, DB.Create(row).Error)
}

func TestValidateModelRedirectInput_NestedRules(t *testing.T) {
	setupModelRedirectTestDB(t)
	insertEnabledChildRedirect(t, "deepseek-flash")

	// Valid nested ref to enabled child.
	err := validateModelRedirectInput(&ModelRedirectInput{
		Name:   "parent",
		Groups: []string{"default"},
		Targets: []ModelRedirectTargetInput{
			{ChannelId: constant.ModelRedirectSentinelChannelID, Model: "deepseek-flash", Priority: 10},
		},
	}, true, "")
	require.NoError(t, err)

	// Self-ref via nested model name.
	err = validateModelRedirectInput(&ModelRedirectInput{
		Name:   "parent",
		Groups: []string{"default"},
		Targets: []ModelRedirectTargetInput{
			{ChannelId: constant.ModelRedirectSentinelChannelID, Model: "parent", Priority: 10},
		},
	}, true, "")
	require.Error(t, err)

	// Nested model required.
	err = validateModelRedirectInput(&ModelRedirectInput{
		Name:   "parent",
		Groups: []string{"default"},
		Targets: []ModelRedirectTargetInput{
			{ChannelId: constant.ModelRedirectSentinelChannelID, Model: "", Priority: 10},
		},
	}, true, "")
	require.Error(t, err)

	// Missing / disabled child.
	err = validateModelRedirectInput(&ModelRedirectInput{
		Name:   "parent",
		Groups: []string{"default"},
		Targets: []ModelRedirectTargetInput{
			{ChannelId: constant.ModelRedirectSentinelChannelID, Model: "no-such-child", Priority: 10},
		},
	}, true, "")
	require.Error(t, err)

	// Invalid channel_id (neither >0 nor sentinel).
	err = validateModelRedirectInput(&ModelRedirectInput{
		Name:   "parent",
		Groups: []string{"default"},
		Targets: []ModelRedirectTargetInput{
			{ChannelId: 0, Model: "x", Priority: 10},
		},
	}, true, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid channel_id")

	err = validateModelRedirectInput(&ModelRedirectInput{
		Name:   "parent",
		Groups: []string{"default"},
		Targets: []ModelRedirectTargetInput{
			{ChannelId: -2, Model: "x", Priority: 10},
		},
	}, true, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid channel_id")
}

func TestValidateModelRedirectInput_Cycle(t *testing.T) {
	setupModelRedirectTestDB(t)

	// Seed a→b so saving b→a forms a cycle.
	now := common.GetTimestamp()
	require.NoError(t, DB.Create(&ModelRedirect{
		Name: "a", Groups: "default", Enabled: true, CreatedAt: now, UpdatedAt: now,
		Targets: []ModelRedirectTarget{
			{ChannelId: constant.ModelRedirectSentinelChannelID, Model: "b", Priority: 10, Enabled: true},
		},
	}).Error)
	require.NoError(t, DB.Create(&ModelRedirect{
		Name: "b", Groups: "default", Enabled: true, CreatedAt: now, UpdatedAt: now,
		Targets: []ModelRedirectTarget{
			// Placeholder real hop so row is valid; cycle check uses input edges for "b".
			{ChannelId: 1, Model: "m1", Priority: 10, Enabled: true},
		},
	}).Error)

	err := validateModelRedirectInput(&ModelRedirectInput{
		Name:   "b",
		Groups: []string{"default"},
		Targets: []ModelRedirectTargetInput{
			{ChannelId: constant.ModelRedirectSentinelChannelID, Model: "a", Priority: 10},
		},
	}, false, "b")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "cycle")
}

func TestOrderRedirectCandidatesWithAffinity_PromotesPreferredInSamePriorityRun(t *testing.T) {
	cands := []RedirectCandidate{
		{ChannelID: 1, Model: "a", Priority: 10},
		{ChannelID: 2, Model: "b", Priority: 10},
		{ChannelID: 3, Model: "c", Priority: 5},
	}
	out := OrderRedirectCandidatesWithAffinity(cands, 2)
	require.Len(t, out, 3)
	require.Equal(t, 2, out[0].ChannelID, "preferred channel must be promoted to front of its run")
	require.Equal(t, 3, out[2].ChannelID, "lower priority run must stay last")
	require.ElementsMatch(t, []int{1, 2}, []int{out[0].ChannelID, out[1].ChannelID})
}

func TestOrderRedirectCandidatesWithAffinity_StablePromotion(t *testing.T) {
	cands := []RedirectCandidate{
		{ChannelID: 1, Priority: 10},
		{ChannelID: 2, Priority: 10},
		{ChannelID: 3, Priority: 10},
	}
	out := OrderRedirectCandidatesWithAffinity(cands, 3)
	require.Len(t, out, 3)
	require.Equal(t, 3, out[0].ChannelID)
	require.Equal(t, 1, out[1].ChannelID, "remaining run members keep relative order")
	require.Equal(t, 2, out[2].ChannelID)
}

func TestOrderRedirectCandidatesWithAffinity_PreferredNotInPool(t *testing.T) {
	cands := []RedirectCandidate{
		{ChannelID: 1, Priority: 10},
		{ChannelID: 2, Priority: 10},
		{ChannelID: 3, Priority: 5},
	}
	out := OrderRedirectCandidatesWithAffinity(cands, 99)
	require.Len(t, out, 3)
	require.Equal(t, 3, out[2].ChannelID)
	require.ElementsMatch(t, []int{1, 2}, []int{out[0].ChannelID, out[1].ChannelID})
}

func TestOrderRedirectCandidatesWithAffinity_OnlySamePriorityRun(t *testing.T) {
	// Black-box expand order [1(P50), 2(P40), 9(P100)] must stick even when the
	// preferred channel sits in a different priority run.
	cands := []RedirectCandidate{
		{ChannelID: 1, Priority: 50},
		{ChannelID: 2, Priority: 40},
		{ChannelID: 9, Priority: 100},
	}
	out := OrderRedirectCandidatesWithAffinity(cands, 2)
	require.Equal(t, []int{1, 2, 9}, []int{out[0].ChannelID, out[1].ChannelID, out[2].ChannelID})
}

func TestOrderRedirectCandidatesWithAffinity_ZeroPreferredMatchesLegacy(t *testing.T) {
	// Multi-element equal-priority run: preferredChannelID <= 0 must delegate to
	// orderRedirectCandidates, which shuffles the run in place while keeping run
	// boundaries and the lower-priority run last. Assert the permutation and run
	// structure rather than an exact order (the shuffle is random), and run enough
	// iterations that both equal-priority members lead sometimes, proving the
	// legacy load-balance path actually ran.
	cands := []RedirectCandidate{
		{ChannelID: 1, Priority: 10},
		{ChannelID: 2, Priority: 10},
		{ChannelID: 3, Priority: 5},
	}
	firstA, firstB := 0, 0
	for i := 0; i < 80; i++ {
		out := OrderRedirectCandidatesWithAffinity(cands, 0)
		require.Len(t, out, 3)
		require.Equal(t, 3, out[2].ChannelID, "lower-priority run must stay last")
		require.ElementsMatch(t, []int{1, 2}, []int{out[0].ChannelID, out[1].ChannelID},
			"equal-priority run must contain exactly its input members")
		switch out[0].ChannelID {
		case 1:
			firstA++
		case 2:
			firstB++
		default:
			require.Failf(t, "unexpected first candidate", "channel id %d", out[0].ChannelID)
		}
	}
	require.True(t, firstA > 0, "legacy equal-share shuffle must let channel 1 lead sometimes")
	require.True(t, firstB > 0, "legacy equal-share shuffle must let channel 2 lead sometimes")
}

func TestOrderRedirectCandidatesWithAffinity_Empty(t *testing.T) {
	require.Nil(t, OrderRedirectCandidatesWithAffinity(nil, 7))
	require.Nil(t, OrderRedirectCandidatesWithAffinity([]RedirectCandidate{}, 7))
}
