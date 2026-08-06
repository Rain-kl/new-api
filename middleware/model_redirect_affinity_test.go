package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	newapii18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupModelRedirectAffinityTest(t *testing.T) {
	t.Helper()
	require.NoError(t, newapii18n.Init())
	prevDB := model.DB
	prevType := common.MainDatabaseType()
	prevRedis := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ModelRedirect{}, &model.ModelRedirectTarget{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = prevDB
		common.SetMainDatabaseType(prevType)
		common.RedisEnabled = prevRedis
		model.InvalidateModelRedirectCache()
	})
}

func seedRedirectAffinityChannels(t *testing.T) {
	t.Helper()
	channels := []model.Channel{
		{Id: 1, Type: 1, Key: "k1", Status: common.ChannelStatusEnabled, Name: "c1", Group: "default", Models: "ha"},
		{Id: 2, Type: 1, Key: "k2", Status: common.ChannelStatusEnabled, Name: "c2", Group: "default", Models: "ha"},
	}
	for i := range channels {
		require.NoError(t, model.DB.Create(&channels[i]).Error)
	}
	redirect := &model.ModelRedirect{
		Name:    "ha",
		Groups:  "default",
		Enabled: true,
		Targets: []model.ModelRedirectTarget{
			{Priority: 100, ChannelId: 1, Enabled: true},
			{Priority: 100, ChannelId: 2, Enabled: true},
		},
	}
	require.NoError(t, model.DB.Create(redirect).Error)
	require.NoError(t, model.LoadModelRedirectCache())
}

func withRedirectAffinityRule(t *testing.T) {
	t.Helper()
	st := operation_setting.GetChannelAffinitySetting()
	prev := *st
	st.Enabled = true
	st.SwitchOnSuccess = true
	st.KeepOnChannelDisabled = false
	st.DefaultTTLSeconds = 3600
	st.Rules = []operation_setting.ChannelAffinityRule{
		{
			Name:               "redirect-test",
			ModelRegex:         []string{"^ha$"},
			KeySources:         []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "session_id"}},
			TTLSeconds:         3600,
			SkipRetryOnFailure: true,
			IncludeUsingGroup:  true,
			IncludeRuleName:    true,
		},
	}
	t.Cleanup(func() { *st = prev })
}

func newRedirectAffinityCtx(sessionID string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("session_id", sessionID)
	c.Request = req
	return c
}

func TestTryModelRedirectSelection_AffinityPromotesBoundChannel(t *testing.T) {
	setupModelRedirectAffinityTest(t)
	seedRedirectAffinityChannels(t)
	withRedirectAffinityRule(t)

	// Seed binding: session "sess-1" on model "ha" prefers channel 2.
	seedCtx := newRedirectAffinityCtx("sess-1")
	_, found := service.GetPreferredChannelByAffinity(seedCtx, "ha", "default")
	require.False(t, found)
	service.RecordChannelAffinity(seedCtx, 2)

	for i := 0; i < 20; i++ {
		c := newRedirectAffinityCtx("sess-1")
		ch, _, _, handled, aborted := tryModelRedirectSelection(c, "ha", "default")
		require.False(t, aborted)
		require.True(t, handled)
		require.NotNil(t, ch)
		require.Equal(t, 2, ch.Id, "bound channel must be promoted within the same-priority pool")

		raw, ok := common.GetContextKey(c, constant.ContextKeyModelRedirectCandidates)
		require.True(t, ok)
		cands := raw.([]model.RedirectCandidate)
		require.Len(t, cands, 2)
		require.Equal(t, 2, cands[0].ChannelID)
		require.Equal(t, 1, cands[1].ChannelID)

		// Promotion runs MarkChannelAffinityUsed (re-applying the rule's
		// meta.SkipRetry) and ClearChannelAffinitySkipRetry must run last so
		// redirect HA is never suppressed by the rule's SkipRetryOnFailure.
		require.False(t, service.ShouldSkipRetryAfterChannelAffinityFailure(c),
			"promotion path must clear skip-retry after marking the channel used")
	}
}

func TestTryModelRedirectSelection_AffinityClearsSkipRetry(t *testing.T) {
	setupModelRedirectAffinityTest(t)
	seedRedirectAffinityChannels(t)
	withRedirectAffinityRule(t)

	c := newRedirectAffinityCtx("sess-2")
	_, found := service.GetPreferredChannelByAffinity(c, "ha", "default")
	require.False(t, found)
	require.True(t, service.ShouldSkipRetryAfterChannelAffinityFailure(c),
		"the affinity rule must establish SkipRetryOnFailure before redirect selection")

	_, _, _, handled, aborted := tryModelRedirectSelection(c, "ha", "default")
	require.False(t, aborted)
	require.True(t, handled)
	require.False(t, service.ShouldSkipRetryAfterChannelAffinityFailure(c),
		"redirect HA must not be suppressed by the rule's SkipRetryOnFailure")
}

func TestTryModelRedirectSelection_AffinityClearsDisabledBinding(t *testing.T) {
	setupModelRedirectAffinityTest(t)
	seedRedirectAffinityChannels(t)
	withRedirectAffinityRule(t)

	seedCtx := newRedirectAffinityCtx("sess-3")
	service.GetPreferredChannelByAffinity(seedCtx, "ha", "default")
	service.RecordChannelAffinity(seedCtx, 2)

	// Disable channel 2 in DB. The wiring checks the channel and falls back to DB
	// when the process-local channel cache is empty (as in tests).
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 2).Update("status", 2).Error)

	c := newRedirectAffinityCtx("sess-3")
	_, _, _, _, aborted := tryModelRedirectSelection(c, "ha", "default")
	require.False(t, aborted)

	// Stale binding must be cleared: a fresh lookup reports not-found.
	checkCtx := newRedirectAffinityCtx("sess-3")
	_, found := service.GetPreferredChannelByAffinity(checkCtx, "ha", "default")
	require.False(t, found)
}

func TestTryModelRedirectSelection_AffinityClearsDisabledBindingWithNoCandidates(t *testing.T) {
	setupModelRedirectAffinityTest(t)
	seedRedirectAffinityChannels(t)
	withRedirectAffinityRule(t)

	seedCtx := newRedirectAffinityCtx("sess-6")
	service.GetPreferredChannelByAffinity(seedCtx, "ha", "default")
	service.RecordChannelAffinity(seedCtx, 2)

	// Disable both channels so candidate filtering returns an empty pool. The
	// disabled preferred binding must still be cleared before the empty return.
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id IN ?", []int{1, 2}).Update("status", 2).Error)

	c := newRedirectAffinityCtx("sess-6")
	_, _, _, handled, aborted := tryModelRedirectSelection(c, "ha", "default")
	require.True(t, handled)
	require.True(t, aborted)

	checkCtx := newRedirectAffinityCtx("sess-6")
	_, found := service.GetPreferredChannelByAffinity(checkCtx, "ha", "default")
	require.False(t, found)
}

func TestTryModelRedirectSelection_AffinityKeepsBindingOnCooldown(t *testing.T) {
	setupModelRedirectAffinityTest(t)
	seedRedirectAffinityChannels(t)
	withRedirectAffinityRule(t)

	seedCtx := newRedirectAffinityCtx("sess-4")
	service.GetPreferredChannelByAffinity(seedCtx, "ha", "default")
	service.RecordChannelAffinity(seedCtx, 2)

	// Cool down channel 2 for this hop. The channel stays enabled, so the
	// affinity binding must survive (only a disabled channel clears it).
	model.RecordModelRedirectHopFailure(2, "ha")
	t.Cleanup(func() { model.ClearModelRedirectHopCooldown(2, "ha") })

	c := newRedirectAffinityCtx("sess-4")
	ch, _, _, handled, aborted := tryModelRedirectSelection(c, "ha", "default")
	require.False(t, aborted)
	require.True(t, handled)
	require.NotNil(t, ch)
	require.Equal(t, 1, ch.Id, "cooled-down preferred channel is skipped for this request")

	checkCtx := newRedirectAffinityCtx("sess-4")
	preferred, found := service.GetPreferredChannelByAffinity(checkCtx, "ha", "default")
	require.True(t, found, "cooldown must not clear the affinity binding")
	require.Equal(t, 2, preferred)
}

func TestTryModelRedirectSelection_AffinityPersistsOnSuccess(t *testing.T) {
	setupModelRedirectAffinityTest(t)
	seedRedirectAffinityChannels(t)
	withRedirectAffinityRule(t)

	precheckCtx := newRedirectAffinityCtx("sess-5")
	_, found := service.GetPreferredChannelByAffinity(precheckCtx, "ha", "default")
	require.False(t, found)

	c := newRedirectAffinityCtx("sess-5")
	ch, _, _, handled, aborted := tryModelRedirectSelection(c, "ha", "default")
	require.False(t, aborted)
	require.True(t, handled)
	require.NotNil(t, ch)

	// Simulate the distributor's success path. The middleware established the
	// affinity context used by RecordChannelAffinity.
	service.RecordChannelAffinity(c, ch.Id)

	checkCtx := newRedirectAffinityCtx("sess-5")
	preferred, found := service.GetPreferredChannelByAffinity(checkCtx, "ha", "default")
	require.True(t, found)
	require.Equal(t, ch.Id, preferred)
}
