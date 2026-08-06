package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateReasoningEffortRules(t *testing.T) {
	t.Parallel()
	require.NoError(t, (*ChannelSettings)(nil).ValidateReasoningEffortRules())
	require.NoError(t, (&ChannelSettings{}).ValidateReasoningEffortRules())
	require.NoError(t, (&ChannelSettings{
		ReasoningEffortRulesEnabled: true,
		ReasoningEffortRules: []ReasoningEffortRule{
			{Model: "gpt-pro[max]", Effort: "max", Force: true},
			{Model: " gpt-pro ", Effort: " high "},
		},
	}).ValidateReasoningEffortRules())

	err := (&ChannelSettings{
		ReasoningEffortRules: []ReasoningEffortRule{{Model: "", Effort: "max"}},
	}).ValidateReasoningEffortRules()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")

	err = (&ChannelSettings{
		ReasoningEffortRules: []ReasoningEffortRule{{Model: "m", Effort: "  "}},
	}).ValidateReasoningEffortRules()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "effort")

	err = (&ChannelSettings{
		ReasoningEffortRules: []ReasoningEffortRule{
			{Model: "a", Effort: "high"},
			{Model: " a ", Effort: "low"},
		},
	}).ValidateReasoningEffortRules()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestReasoningEffortRulesJSONRoundTrip(t *testing.T) {
	t.Parallel()
	raw := `{"reasoning_effort_rules_enabled":true,"reasoning_effort_rules":[{"model":"gpt-pro[max]","effort":"max","force":true}]}`
	var s ChannelSettings
	require.NoError(t, json.Unmarshal([]byte(raw), &s))
	assert.True(t, s.ReasoningEffortRulesEnabled)
	require.Len(t, s.ReasoningEffortRules, 1)
	assert.Equal(t, "gpt-pro[max]", s.ReasoningEffortRules[0].Model)
	assert.Equal(t, "max", s.ReasoningEffortRules[0].Effort)
	assert.True(t, s.ReasoningEffortRules[0].Force)
}
