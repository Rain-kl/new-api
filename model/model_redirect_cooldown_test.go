package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRedirectHopCooldown_ProgressiveAndCap(t *testing.T) {
	resetModelRedirectCooldownsForTest()
	t.Cleanup(resetModelRedirectCooldownsForTest)

	origStep := modelRedirectCooldownStep
	origMax := modelRedirectCooldownMax
	origNow := modelRedirectNow
	t.Cleanup(func() {
		modelRedirectCooldownStep = origStep
		modelRedirectCooldownMax = origMax
		modelRedirectNow = origNow
	})

	modelRedirectCooldownStep = time.Minute
	modelRedirectCooldownMax = 30 * time.Minute
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	now := base
	modelRedirectNow = func() time.Time { return now }

	// 1st failure → 1 minute
	RecordModelRedirectHopFailure(7, "m1")
	require.True(t, IsModelRedirectHopDisabled(7, "m1"))
	now = base.Add(59 * time.Second)
	assert.True(t, IsModelRedirectHopDisabled(7, "m1"))
	now = base.Add(61 * time.Second)
	assert.False(t, IsModelRedirectHopDisabled(7, "m1"))

	// 2nd failure → 2 minutes from this failure time
	now = base.Add(2 * time.Minute)
	RecordModelRedirectHopFailure(7, "m1")
	now = base.Add(2*time.Minute + 119*time.Second)
	assert.True(t, IsModelRedirectHopDisabled(7, "m1"))
	now = base.Add(2*time.Minute + 121*time.Second)
	assert.False(t, IsModelRedirectHopDisabled(7, "m1"))

	// Drive failCount to > 30 → duration capped at 30m
	now = base.Add(10 * time.Minute)
	for i := 0; i < 40; i++ {
		RecordModelRedirectHopFailure(7, "m1")
	}
	// last failure at base+10m, duration max 30m
	now = base.Add(10*time.Minute + 29*time.Minute)
	assert.True(t, IsModelRedirectHopDisabled(7, "m1"))
	now = base.Add(10*time.Minute + 30*time.Minute + time.Second)
	assert.False(t, IsModelRedirectHopDisabled(7, "m1"))
}

func TestModelRedirectHopCooldown_SuccessClears(t *testing.T) {
	resetModelRedirectCooldownsForTest()
	t.Cleanup(resetModelRedirectCooldownsForTest)

	origStep := modelRedirectCooldownStep
	origNow := modelRedirectNow
	t.Cleanup(func() {
		modelRedirectCooldownStep = origStep
		modelRedirectNow = origNow
	})
	modelRedirectCooldownStep = time.Minute
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	modelRedirectNow = func() time.Time { return base }

	RecordModelRedirectHopFailure(3, "x")
	require.True(t, IsModelRedirectHopDisabled(3, "x"))
	ClearModelRedirectHopCooldown(3, "x")
	assert.False(t, IsModelRedirectHopDisabled(3, "x"))

	// After clear, next failure is again 1m (failCount reset)
	RecordModelRedirectHopFailure(3, "x")
	modelRedirectNow = func() time.Time { return base.Add(61 * time.Second) }
	assert.False(t, IsModelRedirectHopDisabled(3, "x"))
}

func TestFilterRedirectCooldownDown(t *testing.T) {
	resetModelRedirectCooldownsForTest()
	t.Cleanup(resetModelRedirectCooldownsForTest)

	origStep := modelRedirectCooldownStep
	origNow := modelRedirectNow
	t.Cleanup(func() {
		modelRedirectCooldownStep = origStep
		modelRedirectNow = origNow
	})
	modelRedirectCooldownStep = time.Minute
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	modelRedirectNow = func() time.Time { return base }

	RecordModelRedirectHopFailure(1, "m1")

	cands := []RedirectCandidate{
		{ChannelID: 1, Model: "m1", Priority: 100},
		{ChannelID: 2, Model: "m2", Priority: 50},
	}
	out := FilterRedirectCooldownDown(cands, "virtual")
	require.Len(t, out, 1)
	assert.Equal(t, 2, out[0].ChannelID)
}

func TestFilterRedirectCooldownDown_PassthroughAndKeyIsolation(t *testing.T) {
	resetModelRedirectCooldownsForTest()
	t.Cleanup(resetModelRedirectCooldownsForTest)

	origStep := modelRedirectCooldownStep
	origNow := modelRedirectNow
	t.Cleanup(func() {
		modelRedirectCooldownStep = origStep
		modelRedirectNow = origNow
	})
	modelRedirectCooldownStep = time.Minute
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	modelRedirectNow = func() time.Time { return base }

	// Cool hop (ch=1, attempt model = client virtual name via empty Model).
	RecordModelRedirectHopFailure(1, "virtual")
	// Different model on same channel stays available.
	RecordModelRedirectHopFailure(2, "other")

	cands := []RedirectCandidate{
		{ChannelID: 1, Model: "", Priority: 100},        // AttemptModel → "virtual" → cooled
		{ChannelID: 1, Model: "explicit", Priority: 90}, // different key
		{ChannelID: 2, Model: "other", Priority: 80},    // cooled
		{ChannelID: 3, Model: "ok", Priority: 70},
	}
	out := FilterRedirectCooldownDown(cands, "virtual")
	require.Len(t, out, 2)
	assert.Equal(t, 1, out[0].ChannelID)
	assert.Equal(t, "explicit", out[0].Model)
	assert.Equal(t, 3, out[1].ChannelID)
}

func TestIsModelRedirectHopDisabled_CopiesUntilUnderLock(t *testing.T) {
	// Smoke: concurrent Record + IsDisabled must not race (run with -race).
	resetModelRedirectCooldownsForTest()
	t.Cleanup(resetModelRedirectCooldownsForTest)

	origStep := modelRedirectCooldownStep
	origNow := modelRedirectNow
	t.Cleanup(func() {
		modelRedirectCooldownStep = origStep
		modelRedirectNow = origNow
	})
	modelRedirectCooldownStep = time.Minute
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	modelRedirectNow = func() time.Time { return base }

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			RecordModelRedirectHopFailure(9, "race")
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		_ = IsModelRedirectHopDisabled(9, "race")
	}
	<-done
	assert.True(t, IsModelRedirectHopDisabled(9, "race"))
}

func TestRecordModelRedirectHopFailure_IgnoresNonPositiveChannel(t *testing.T) {
	resetModelRedirectCooldownsForTest()
	t.Cleanup(resetModelRedirectCooldownsForTest)
	RecordModelRedirectHopFailure(0, "x")
	RecordModelRedirectHopFailure(-1, "x")
	assert.False(t, IsModelRedirectHopDisabled(0, "x"))
	assert.False(t, IsModelRedirectHopDisabled(-1, "x"))
}

func TestFilterRedirectCooldownDown_KeepsModelOnly(t *testing.T) {
	cands := []RedirectCandidate{
		{ChannelID: 0, Model: "gpt-5.6", Priority: 100},
	}
	out := FilterRedirectCooldownDown(cands, "auto")
	require.Len(t, out, 1)
	assert.Equal(t, "gpt-5.6", out[0].Model)
}

// Regression: a model-only redirect candidate whose top-priority channel is in
// hop cooldown must fall through to the next channel-priority tier instead of
// skipping the whole model and moving on to the next redirect target.
func TestFirstUsableRedirectCandidate_ModelOnlyWalksTiersPastCooldown(t *testing.T) {
	setupModelRedirectTestDB(t)
	resetModelRedirectCooldownsForTest()
	t.Cleanup(resetModelRedirectCooldownsForTest)

	origStep := modelRedirectCooldownStep
	origNow := modelRedirectNow
	t.Cleanup(func() {
		modelRedirectCooldownStep = origStep
		modelRedirectNow = origNow
	})
	modelRedirectCooldownStep = time.Minute
	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	modelRedirectNow = func() time.Time { return base }

	prevMem := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false // select channels via the DB (abilities) path
	t.Cleanup(func() { common.MemoryCacheEnabled = prevMem })

	// Top-priority channel 9011 (will be cooled) and lower-priority channel 9012,
	// both serving model "gpt-5.6" for group "default".
	require.NoError(t, DB.Create(&Channel{Id: 9011, Name: "c-top", Type: 1, Key: "k1", Models: "gpt-5.6", Group: "default"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 9012, Name: "c-low", Type: 1, Key: "k2", Models: "gpt-5.6", Group: "default"}).Error)
	t.Cleanup(func() {
		_ = DB.Where("channel_id IN ?", []int{9011, 9012}).Delete(&Ability{}).Error
		_ = DB.Where("id IN ?", []int{9011, 9012}).Delete(&Channel{}).Error
	})

	top := int64(10)
	low := int64(0)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "gpt-5.6", ChannelId: 9011, Enabled: true, Priority: &top, Weight: 10}).Error)
	require.NoError(t, DB.Create(&Ability{Group: "default", Model: "gpt-5.6", ChannelId: 9012, Enabled: true, Priority: &low, Weight: 10}).Error)

	// Cool the top-priority hop only; the lower tier must remain reachable.
	RecordModelRedirectHopFailure(9011, "gpt-5.6")

	cands := []RedirectCandidate{
		{ChannelID: 0, Model: "gpt-5.6", Priority: 100},
		{ChannelID: 0, Model: "claude-5", Priority: 50},
	}

	ch, model, idx := FirstUsableRedirectCandidate(cands, "flash", "default", "/v1/chat/completions", 2)
	require.NotNil(t, ch)
	assert.Equal(t, 9012, ch.Id)
	assert.Equal(t, "gpt-5.6", model)
	assert.Equal(t, 0, idx)
}
