package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClearChannelAffinitySkipRetry(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// Rule-level SkipRetryOnFailure is carried by the affinity meta.
	setChannelAffinityContext(c, channelAffinityMeta{SkipRetry: true})
	require.True(t, ShouldSkipRetryAfterChannelAffinityFailure(c), "control: meta skip-retry must apply before clearing")

	ClearChannelAffinitySkipRetry(c)
	require.False(t, ShouldSkipRetryAfterChannelAffinityFailure(c), "helper must force skip-retry off")
}

func TestClearChannelAffinitySkipRetry_NilContext(t *testing.T) {
	require.NotPanics(t, func() { ClearChannelAffinitySkipRetry(nil) })
}

func TestApplyChannelAffinityOverrideTemplate_RedirectActiveSkipsTemplate(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setChannelAffinityContext(c, channelAffinityMeta{
		ParamTemplate: map[string]interface{}{
			"headers": map[string]interface{}{"x-pass": "1"},
		},
	})
	common.SetContextKey(c, constant.ContextKeyModelRedirectActive, true)

	base := map[string]interface{}{"model": "gpt-4o"}
	got, applied := ApplyChannelAffinityOverrideTemplate(c, base)
	require.False(t, applied, "redirect requests must stay affinity-only")
	require.Equal(t, base, got, "redirect requests must get the unmodified base override")
	require.NotContains(t, got, "headers", "rule param_override_template must not be injected on redirect requests")
}

func TestApplyChannelAffinityOverrideTemplate_AppliesWhenNotRedirect(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setChannelAffinityContext(c, channelAffinityMeta{
		ParamTemplate: map[string]interface{}{
			"headers": map[string]interface{}{"x-pass": "1"},
		},
	})

	base := map[string]interface{}{"model": "gpt-4o"}
	got, applied := ApplyChannelAffinityOverrideTemplate(c, base)
	require.True(t, applied, "non-redirect affinity requests must apply the matched rule template")
	require.Equal(t, "1", got["headers"].(map[string]interface{})["x-pass"], "template value must be merged")
	require.Equal(t, "gpt-4o", got["model"], "existing base override keys must be preserved")
}
