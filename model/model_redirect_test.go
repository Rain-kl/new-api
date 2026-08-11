package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
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

func TestRedirectCandidate_IsModelOnly(t *testing.T) {
	require.True(t, RedirectCandidate{ChannelID: 0, Model: "gpt-5.6"}.IsModelOnly())
	require.False(t, RedirectCandidate{ChannelID: 0, Model: " "}.IsModelOnly())
	require.False(t, RedirectCandidate{ChannelID: 1, Model: "gpt-5.6"}.IsModelOnly())
	require.False(t, RedirectCandidate{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "x"}.IsModelOnly())
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

func TestFilterRedirectCandidates_KeepsModelOnly(t *testing.T) {
	cands := []RedirectCandidate{
		{ChannelID: 0, Model: "gpt-5.6", Priority: 100},
		{ChannelID: 999999, Model: "", Priority: 90}, // missing channel -> dropped
	}
	out := FilterRedirectCandidates(cands, "auto", "default", "/v1/chat/completions", nil)
	require.Len(t, out, 1)
	assert.Equal(t, "gpt-5.6", out[0].Model)
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

func TestBuildModelRedirectCacheMap_MappingEntry(t *testing.T) {
	setupModelRedirectTestDB(t)
	now := common.GetTimestamp()
	require.NoError(t, DB.Create(&ModelRedirect{
		Name: "auto", Groups: "default", Enabled: true,
		Mode: ModelRedirectModeMapping, MappingTarget: "gpt-5.6",
		CreatedAt: now, UpdatedAt: now,
	}).Error)
	require.NoError(t, DB.Create(&ModelRedirect{
		Name: "ha", Groups: "default", Enabled: true,
		CreatedAt: now, UpdatedAt: now,
		Targets: []ModelRedirectTarget{
			{ChannelId: 1, Model: "gpt-4o", Priority: 100, Enabled: true},
		},
	}).Error)
	// Mapping entry with empty target must be skipped.
	require.NoError(t, DB.Create(&ModelRedirect{
		Name: "bad", Groups: "default", Enabled: true,
		Mode: ModelRedirectModeMapping, MappingTarget: "",
		CreatedAt: now, UpdatedAt: now,
	}).Error)

	m, err := buildModelRedirectCacheMap()
	require.NoError(t, err)

	auto := m["auto"]
	require.NotNil(t, auto)
	require.Equal(t, ModelRedirectModeMapping, auto.Mode)
	require.Equal(t, "gpt-5.6", auto.MappingTarget)
	require.Empty(t, auto.Targets)

	ha := m["ha"]
	require.NotNil(t, ha)
	require.Equal(t, ModelRedirectModeRedirect, ha.Mode)
	require.Len(t, ha.Targets, 1)

	_, ok := m["bad"]
	require.False(t, ok)
}

func TestModelRedirectTarget_EnabledRoundTrip(t *testing.T) {
	setupModelRedirectTestDB(t)
	// High ids avoid collisions with channel rows other tests leave in the shared DB.
	require.NoError(t, DB.Create(&Channel{Id: 9101, Name: "c1", Type: 1, Key: "k1", Models: "gpt-4o"}).Error)
	t.Cleanup(func() {
		_ = DB.Where("id = ?", 9101).Delete(&Channel{}).Error
	})

	// Create with one disabled target.
	created, err := CreateModelRedirect(&ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeRedirect,
		Targets: []ModelRedirectTargetInput{
			{ChannelId: 9101, Model: "gpt-4o", Priority: 100, Enabled: common.GetPointer(true)},
			{ChannelId: 9101, Model: "gpt-4o", Priority: 90, Enabled: common.GetPointer(false)},
		},
	})
	require.NoError(t, err)
	var t90 ModelRedirectTarget
	require.NoError(t, DB.Where("redirect_id = ? AND priority = ?", created.Id, 90).First(&t90).Error)
	require.False(t, t90.Enabled, "disabled target must be stored disabled on create")

	// Update: flip the flags (100 disabled, 90 enabled).
	updated, err := UpdateModelRedirect(created.Id, &ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeRedirect,
		Targets: []ModelRedirectTargetInput{
			{ChannelId: 9101, Model: "gpt-4o", Priority: 100, Enabled: common.GetPointer(false)},
			{ChannelId: 9101, Model: "gpt-4o", Priority: 90, Enabled: common.GetPointer(true)},
		},
	})
	require.NoError(t, err)
	require.Len(t, updated.Targets, 2)
	var t100 ModelRedirectTarget
	require.NoError(t, DB.Where("redirect_id = ? AND priority = ?", created.Id, 100).First(&t100).Error)
	require.False(t, t100.Enabled, "disabled target must be stored disabled on update")
	var t90b ModelRedirectTarget
	require.NoError(t, DB.Where("redirect_id = ? AND priority = ?", created.Id, 90).First(&t90b).Error)
	require.True(t, t90b.Enabled)

	// Cache only routes enabled targets.
	require.NoError(t, LoadModelRedirectCache())
	cands, ok := ResolveModelRedirect("auto", "default")
	require.True(t, ok)
	require.Len(t, cands, 1)
	assert.Equal(t, 90, cands[0].Priority)
}

func TestGetRandomSatisfiedChannel_PlaygroundPathNeedsNormalization(t *testing.T) {
	setupModelRedirectTestDB(t)
	prevMem := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMem
		_ = DB.Where("id = ?", 9201).Delete(&Channel{}).Error
	})

	// Advanced Custom (type 58) channel serving deepseek-flash with a
	// /v1/chat/completions route — the standard route config.
	ch := &Channel{
		Id: 9201, Type: constant.ChannelTypeAdvancedCustom, Key: "k",
		Status: common.ChannelStatusEnabled, Name: "ac", Group: "default", Models: "deepseek-flash",
	}
	ch.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/chat/completions", Models: []string{"deepseek-flash"}},
			},
		},
	})
	require.NoError(t, DB.Create(ch).Error)
	// InitChannelCache derives group keys from the abilities table; seed one so
	// the (default, deepseek-flash) bucket exists.
	prio := int64(10)
	require.NoError(t, DB.Create(&Ability{
		Group: "default", Model: "deepseek-flash", ChannelId: 9201, Enabled: true, Priority: &prio, Weight: 10,
	}).Error)
	t.Cleanup(func() { _ = DB.Where("channel_id = ?", 9201).Delete(&Ability{}).Error })
	InitChannelCache()

	// The distributor normalizes the playground path to /v1/... before selection,
	// so the Advanced Custom route matches and the channel is selected.
	got, err := GetRandomSatisfiedChannel("default", "deepseek-flash", 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, got, "normalized /v1 path must match the Advanced Custom route")
	require.Equal(t, 9201, got.Id)

	// The raw playground path does NOT match the exact route — this is exactly why
	// Distribute normalizes the selection path.
	raw, err := GetRandomSatisfiedChannel("default", "deepseek-flash", 0, "/pg/chat/completions")
	require.NoError(t, err)
	require.Nil(t, raw, "raw /pg path must not match; the distributor normalizes before selection")
}

func TestCreateModelRedirect_DisabledEntryStaysDisabled(t *testing.T) {
	setupModelRedirectTestDB(t)
	require.NoError(t, DB.Create(&Channel{Id: 9102, Name: "c2", Type: 1, Key: "k2", Models: "gpt-4o"}).Error)
	t.Cleanup(func() {
		_ = DB.Where("id = ?", 9102).Delete(&Channel{}).Error
	})

	created, err := CreateModelRedirect(&ModelRedirectInput{
		Name: "off", Groups: []string{"default"}, Enabled: common.GetPointer(false),
		Mode: ModelRedirectModeRedirect,
		Targets: []ModelRedirectTargetInput{
			{ChannelId: 9102, Model: "gpt-4o", Priority: 100, Enabled: common.GetPointer(true)},
		},
	})
	require.NoError(t, err)
	require.False(t, created.Enabled, "disabled entry must be stored disabled on create")

	require.NoError(t, LoadModelRedirectCache())
	if _, ok := ResolveModelRedirect("off", "default"); ok {
		t.Fatal("disabled entry must not resolve")
	}
}

func TestResolveModelRedirect_MappingDirect(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"auto": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "gpt-5.6",
			Groups:        map[string]struct{}{"default": {}},
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

	cands, ok := ResolveModelRedirect("auto", "default")
	require.True(t, ok)
	require.Len(t, cands, 1)
	require.True(t, cands[0].IsModelOnly())
	assert.Equal(t, "gpt-5.6", cands[0].Model)
	assert.Equal(t, modelRedirectDefaultPrio, cands[0].Priority)
}

func TestResolveModelRedirect_MappingGroupGate(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"auto": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "gpt-5.6",
			Groups:        map[string]struct{}{"vip": {}},
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

	if _, ok := ResolveModelRedirect("auto", "default"); ok {
		t.Fatal("expected miss for wrong group")
	}
}

func TestResolveModelRedirect_RedirectNestedMapping(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"gpt": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "gpt-5.6",
			Groups:        map[string]struct{}{"default": {}},
		},
		"claude": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "claude-5",
			Groups:        map[string]struct{}{"default": {}},
		},
		"auto": {
			Mode:   ModelRedirectModeRedirect,
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "gpt", Priority: 100},
				{ChannelID: constant.ModelRedirectSentinelChannelID, Model: "claude", Priority: 90},
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

	cands, ok := ResolveModelRedirect("auto", "default")
	require.True(t, ok)
	require.Len(t, cands, 2)
	require.True(t, cands[0].IsModelOnly())
	require.True(t, cands[1].IsModelOnly())
	assert.Equal(t, "gpt-5.6", cands[0].Model)
	assert.Equal(t, 100, cands[0].Priority, "nested mapping inherits the sentinel priority")
	assert.Equal(t, "claude-5", cands[1].Model)
	assert.Equal(t, 90, cands[1].Priority)
}

func TestResolveModelRedirect_MappingChain(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"a": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "b",
			Groups:        map[string]struct{}{"default": {}},
		},
		"b": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "gpt-5.6",
			Groups:        map[string]struct{}{"default": {}},
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

	cands, ok := ResolveModelRedirect("a", "default")
	require.True(t, ok)
	require.Len(t, cands, 1)
	require.True(t, cands[0].IsModelOnly())
	assert.Equal(t, "gpt-5.6", cands[0].Model)
}

func TestResolveModelRedirect_MappingToRedirect(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"gpt": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "ha",
			Groups:        map[string]struct{}{"default": {}},
		},
		"ha": {
			Mode:   ModelRedirectModeRedirect,
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: 1, Model: "gpt-4o", Priority: 100},
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

	cands, ok := ResolveModelRedirect("gpt", "default")
	require.True(t, ok)
	require.Len(t, cands, 1)
	require.False(t, cands[0].IsModelOnly())
	assert.Equal(t, 1, cands[0].ChannelID)
	assert.Equal(t, "gpt-4o", cands[0].Model)
}

func TestResolveModelRedirect_MappingCycleTerminates(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"a": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "b",
			Groups:        map[string]struct{}{"default": {}},
		},
		"b": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "a",
			Groups:        map[string]struct{}{"default": {}},
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
	require.False(t, ok, "mapping cycle must terminate without candidates")
}

func TestValidateModelRedirectInput_Mapping(t *testing.T) {
	setupModelRedirectTestDB(t)

	// mapping mode requires a target
	err := validateModelRedirectInput(&ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeMapping, MappingTarget: "",
	}, true, "")
	require.Error(t, err)

	// valid mapping entry
	err = validateModelRedirectInput(&ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeMapping, MappingTarget: "gpt-5.6",
	}, true, "")
	require.NoError(t, err)

	// invalid mode
	err = validateModelRedirectInput(&ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: "bogus", MappingTarget: "gpt-5.6",
	}, true, "")
	require.Error(t, err)

	// mapping target must not contain commas / control whitespace
	err = validateModelRedirectInput(&ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeMapping, MappingTarget: "a,b",
	}, true, "")
	require.Error(t, err)

	// redirect mode ignores mapping target and still requires targets
	err = validateModelRedirectInput(&ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeRedirect, MappingTarget: "gpt-5.6",
	}, true, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target")
}

func TestValidateModelRedirectInput_MappingCycle(t *testing.T) {
	setupModelRedirectTestDB(t)
	now := common.GetTimestamp()
	// Seed a -> b (mapping edge); saving b -> a must be a cycle.
	require.NoError(t, DB.Create(&ModelRedirect{
		Name: "a", Groups: "default", Enabled: true, Mode: ModelRedirectModeMapping,
		MappingTarget: "b", CreatedAt: now, UpdatedAt: now,
	}).Error)

	err := validateModelRedirectInput(&ModelRedirectInput{
		Name: "b", Groups: []string{"default"}, Mode: ModelRedirectModeMapping, MappingTarget: "a",
	}, true, "")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "cycle")

	// self-reference is a cycle
	err = validateModelRedirectInput(&ModelRedirectInput{
		Name: "c", Groups: []string{"default"}, Mode: ModelRedirectModeMapping, MappingTarget: "c",
	}, true, "")
	require.Error(t, err)
}

func TestValidateModelRedirectInput_MappingTargetDisabled(t *testing.T) {
	setupModelRedirectTestDB(t)
	now := common.GetTimestamp()
	ha := &ModelRedirect{
		Name: "ha", Groups: "default", Enabled: true, Mode: ModelRedirectModeMapping,
		MappingTarget: "gpt-5.6", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, DB.Create(ha).Error)
	// Enabled has gorm:"default:true", so Create omits a false zero value; set
	// the disabled state explicitly so the disabled-target check is exercised.
	require.NoError(t, DB.Model(&ModelRedirect{}).Where("id = ?", ha.Id).Update("enabled", false).Error)

	err := validateModelRedirectInput(&ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeMapping, MappingTarget: "ha",
	}, true, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestUpdateModelRedirect_PreservesInactiveConfig(t *testing.T) {
	setupModelRedirectTestDB(t)
	// High ids avoid collisions with channel rows other tests leave in the shared DB.
	require.NoError(t, DB.Create(&Channel{Id: 9001, Name: "c1", Type: 1, Key: "k1", Models: "gpt-4o,claude"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 9002, Name: "c2", Type: 1, Key: "k2", Models: "claude"}).Error)
	t.Cleanup(func() {
		_ = DB.Where("id IN ?", []int{9001, 9002}).Delete(&Channel{}).Error
	})

	created, err := CreateModelRedirect(&ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeRedirect,
		Targets: []ModelRedirectTargetInput{
			{ChannelId: 9001, Model: "gpt-4o", Priority: 100, Enabled: common.GetPointer(true)},
		},
	})
	require.NoError(t, err)
	require.Equal(t, ModelRedirectModeRedirect, created.Mode)
	require.Len(t, created.Targets, 1)

	// Switch to mapping mode: stored targets must survive.
	updated, err := UpdateModelRedirect(created.Id, &ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeMapping, MappingTarget: "gpt-5.6",
	})
	require.NoError(t, err)
	require.Equal(t, ModelRedirectModeMapping, updated.Mode)
	require.Equal(t, "gpt-5.6", updated.MappingTarget)
	require.Len(t, updated.Targets, 1, "redirect targets must survive a switch to mapping mode")

	// Switch back to redirect: stored mapping target must survive, targets replaced.
	updated2, err := UpdateModelRedirect(created.Id, &ModelRedirectInput{
		Name: "auto", Groups: []string{"default"}, Mode: ModelRedirectModeRedirect,
		Targets: []ModelRedirectTargetInput{
			{ChannelId: 9002, Model: "claude", Priority: 80, Enabled: common.GetPointer(true)},
		},
	})
	require.NoError(t, err)
	require.Equal(t, ModelRedirectModeRedirect, updated2.Mode)
	require.Equal(t, "gpt-5.6", updated2.MappingTarget, "mapping target must survive a switch back to redirect mode")
	require.Len(t, updated2.Targets, 1)
	assert.Equal(t, 9002, updated2.Targets[0].ChannelId)
}

func TestModelRedirectDisplaySourceModel_Mapping(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"auto": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "gpt-5.6",
			Groups:        map[string]struct{}{"default": {}},
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

	assert.Equal(t, "gpt-5.6", ModelRedirectDisplaySourceModel("auto"))
}

func TestForEachEnabledModelRedirect_IncludesMapping(t *testing.T) {
	modelRedirectCacheMu.Lock()
	modelRedirectCache = map[string]*modelRedirectCacheEntry{
		"auto": {
			Mode:          ModelRedirectModeMapping,
			MappingTarget: "gpt-5.6",
			Groups:        map[string]struct{}{"default": {}},
		},
		"ha": {
			Mode:   ModelRedirectModeRedirect,
			Groups: map[string]struct{}{"default": {}},
			Targets: []RedirectCandidate{
				{ChannelID: 1, Model: "gpt-4o", Priority: 100},
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

	got := map[string]string{}
	ForEachEnabledModelRedirect(func(name string, _ []string, displaySource string) {
		got[name] = displaySource
	})
	assert.Equal(t, "gpt-5.6", got["auto"])
	assert.Equal(t, "gpt-4o", got["ha"])
}

func TestRedirectSlotMapping(t *testing.T) {
	cands := []RedirectCandidate{
		{ChannelID: 1, Model: "a", Priority: 10},       // channel-bound: 1 slot
		{ChannelID: 0, Model: "gpt-5.6", Priority: 5},  // model-only: 3 slots
		{ChannelID: 0, Model: "claude-5", Priority: 4}, // model-only: 3 slots
	}
	// slot ranges: cand0 [0], cand1 [1,2,3], cand2 [4,5,6]
	cases := []struct {
		slot, candIdx, level int
	}{
		{0, 0, 0},
		{1, 1, 0},
		{2, 1, 1},
		{3, 1, 2},
		{4, 2, 0},
		{5, 2, 1},
		{6, 2, 2},
	}
	for _, tc := range cases {
		idx, lvl, ok := redirectSlotMapping(cands, tc.slot, 3)
		require.True(t, ok, "slot %d", tc.slot)
		assert.Equal(t, tc.candIdx, idx, "slot %d", tc.slot)
		assert.Equal(t, tc.level, lvl, "slot %d", tc.slot)
	}
	if _, _, ok := redirectSlotMapping(cands, 7, 3); ok {
		t.Fatal("slot 7 must be out of range")
	}
	if _, _, ok := redirectSlotMapping(nil, 0, 3); ok {
		t.Fatal("empty candidate list must be out of range")
	}
}

func TestResolveRedirectSlot_ChannelBound(t *testing.T) {
	setupModelRedirectTestDB(t)
	require.NoError(t, DB.Create(&Channel{Id: 9007, Name: "c7", Type: 1, Key: "k7", Models: "gpt-4o"}).Error)
	t.Cleanup(func() {
		_ = DB.Where("id = ?", 9007).Delete(&Channel{}).Error
	})
	cands := []RedirectCandidate{
		{ChannelID: 9007, Model: "gpt-4o", Priority: 10},
	}
	ch, model := ResolveRedirectSlot(cands, 0, "ha", "default", "/v1/chat/completions", 3)
	require.NotNil(t, ch)
	assert.Equal(t, 9007, ch.Id)
	assert.Equal(t, "gpt-4o", model)

	ch2, _ := ResolveRedirectSlot(cands, 1, "ha", "default", "/v1/chat/completions", 3)
	require.Nil(t, ch2, "channel-bound candidate spans exactly one slot")
}

func TestResolveRedirectSlot_ModelOnlyProbe(t *testing.T) {
	setupModelRedirectTestDB(t)
	prevMem := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false // tests select channels via the DB (abilities) path
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMem
		_ = DB.Where("id = ?", 9003).Delete(&Channel{}).Error
	})
	require.NoError(t, DB.Create(&Channel{Id: 9003, Name: "c3", Type: 1, Key: "k3", Models: "gpt-5.6", Group: "default"}).Error)
	prio := int64(10)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "gpt-5.6", ChannelId: 9003, Enabled: true, Priority: &prio, Weight: 10}).Error)

	cands := []RedirectCandidate{
		{ChannelID: 0, Model: "gpt-5.6", Priority: 5},
	}
	// slot 0 -> tier 0 selects the seeded channel
	ch, model := ResolveRedirectSlot(cands, 0, "auto", "default", "/v1/chat/completions", 3)
	require.NotNil(t, ch)
	assert.Equal(t, 9003, ch.Id)
	assert.Equal(t, "gpt-5.6", model)
}
