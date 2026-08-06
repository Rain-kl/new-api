package common

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveChannelReasoningEffort(t *testing.T) {
	t.Parallel()
	settings := dto.ChannelSettings{
		ReasoningEffortRulesEnabled: true,
		ReasoningEffortRules: []dto.ReasoningEffortRule{
			{Model: "gpt-pro[max]", Effort: "max", Force: true},
			{Model: "gpt-pro", Effort: "high", Force: false},
		},
	}

	effort, force, apply := ResolveChannelReasoningEffort(settings, "gpt-pro[max]", true)
	assert.True(t, apply)
	assert.True(t, force)
	assert.Equal(t, "max", effort)

	effort, force, apply = ResolveChannelReasoningEffort(settings, "gpt-pro", true)
	assert.False(t, apply)

	effort, force, apply = ResolveChannelReasoningEffort(settings, "gpt-pro", false)
	assert.True(t, apply)
	assert.False(t, force)
	assert.Equal(t, "high", effort)

	_, _, apply = ResolveChannelReasoningEffort(dto.ChannelSettings{
		ReasoningEffortRulesEnabled: false,
		ReasoningEffortRules:        settings.ReasoningEffortRules,
	}, "gpt-pro[max]", false)
	assert.False(t, apply)

	_, _, apply = ResolveChannelReasoningEffort(settings, "other", false)
	assert.False(t, apply)
}

func TestApplyChannelReasoningEffortChat_ForceAndDefault(t *testing.T) {
	t.Parallel()
	settings := dto.ChannelSettings{
		ReasoningEffortRulesEnabled: true,
		ReasoningEffortRules: []dto.ReasoningEffortRule{
			{Model: "gpt-pro[max]", Effort: "max", Force: true},
			{Model: "gpt-pro", Effort: "high", Force: false},
		},
	}

	info := &RelayInfo{
		OriginModelName: "gpt-pro[max]",
		ChannelMeta:     &ChannelMeta{ChannelSetting: settings},
	}
	req := &dto.GeneralOpenAIRequest{ReasoningEffort: "low"}
	ApplyChannelReasoningEffortChat(info, req)
	assert.Equal(t, "max", req.ReasoningEffort)
	assert.True(t, info.ReasoningEffortFromChannel)
	assert.Equal(t, "max", info.GetReasoningEffort())

	info2 := &RelayInfo{
		OriginModelName: "gpt-pro",
		ChannelMeta:     &ChannelMeta{ChannelSetting: settings},
	}
	req2 := &dto.GeneralOpenAIRequest{ReasoningEffort: "low"}
	ApplyChannelReasoningEffortChat(info2, req2)
	assert.Equal(t, "low", req2.ReasoningEffort)
	assert.False(t, info2.ReasoningEffortFromChannel)

	info3 := &RelayInfo{
		OriginModelName: "gpt-pro",
		ChannelMeta:     &ChannelMeta{ChannelSetting: settings},
	}
	req3 := &dto.GeneralOpenAIRequest{}
	ApplyChannelReasoningEffortChat(info3, req3)
	assert.Equal(t, "high", req3.ReasoningEffort)
	assert.True(t, info3.ReasoningEffortFromChannel)
}

func TestApplyChannelReasoningEffortResponses(t *testing.T) {
	t.Parallel()
	settings := dto.ChannelSettings{
		ReasoningEffortRulesEnabled: true,
		ReasoningEffortRules: []dto.ReasoningEffortRule{
			{Model: "o3", Effort: "xhigh", Force: true},
		},
	}
	info := &RelayInfo{
		OriginModelName: "o3",
		ChannelMeta:     &ChannelMeta{ChannelSetting: settings},
	}
	req := &dto.OpenAIResponsesRequest{
		Reasoning: &dto.Reasoning{Summary: "auto"},
	}
	ApplyChannelReasoningEffortResponses(info, req)
	require.NotNil(t, req.Reasoning)
	assert.Equal(t, "xhigh", req.Reasoning.Effort)
	assert.Equal(t, "auto", req.Reasoning.Summary)
}

func TestApplyChannelReasoningEffortClaude_MergesOutputConfig(t *testing.T) {
	t.Parallel()
	settings := dto.ChannelSettings{
		ReasoningEffortRulesEnabled: true,
		ReasoningEffortRules: []dto.ReasoningEffortRule{
			{Model: "claude-opus", Effort: "max", Force: true},
		},
	}
	info := &RelayInfo{
		OriginModelName: "claude-opus",
		ChannelMeta:     &ChannelMeta{ChannelSetting: settings},
	}
	req := &dto.ClaudeRequest{
		OutputConfig: json.RawMessage(`{"foo":"bar"}`),
	}
	ApplyChannelReasoningEffortClaude(info, req)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(req.OutputConfig, &cfg))
	assert.Equal(t, "max", cfg["effort"])
	assert.Equal(t, "bar", cfg["foo"])
}

func TestApplyChannelReasoningEffortClaude_ClientThinkingCountsAsSpecified(t *testing.T) {
	t.Parallel()
	settings := dto.ChannelSettings{
		ReasoningEffortRulesEnabled: true,
		ReasoningEffortRules: []dto.ReasoningEffortRule{
			{Model: "claude", Effort: "high", Force: false},
		},
	}
	info := &RelayInfo{
		OriginModelName: "claude",
		ChannelMeta:     &ChannelMeta{ChannelSetting: settings},
	}
	req := &dto.ClaudeRequest{
		Thinking: &dto.Thinking{Type: "enabled"},
	}
	ApplyChannelReasoningEffortClaude(info, req)
	assert.False(t, info.ReasoningEffortFromChannel)
	assert.Empty(t, req.GetEfforts())
}
