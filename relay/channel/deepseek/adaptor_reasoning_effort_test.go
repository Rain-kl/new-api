package deepseek

import (
	"encoding/json"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyDeepSeekV4OpenAIThinkingSuffix_FromChannelStripsOnly(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "deepseek-v4-pro-max",
		},
		ReasoningEffort:            "high",
		ReasoningEffortFromChannel: true,
	}
	req := &dto.GeneralOpenAIRequest{
		Model:           "deepseek-v4-pro-max",
		ReasoningEffort: "high",
	}

	err := applyDeepSeekV4OpenAIThinkingSuffix(info, req)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-v4-pro", req.Model)
	assert.Equal(t, "deepseek-v4-pro", info.UpstreamModelName)
	assert.Equal(t, "high", req.ReasoningEffort)
	assert.Equal(t, "high", info.ReasoningEffort)
	assert.Nil(t, req.THINKING)
}

func TestApplyDeepSeekV4OpenAIThinkingSuffix_AppliesWhenNotFromChannel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "deepseek-v4-pro-max",
		},
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "deepseek-v4-pro-max",
	}

	err := applyDeepSeekV4OpenAIThinkingSuffix(info, req)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-v4-pro", req.Model)
	assert.Equal(t, "max", req.ReasoningEffort)
	assert.Equal(t, "max", info.ReasoningEffort)
	require.NotNil(t, req.THINKING)
	var thinking map[string]string
	require.NoError(t, json.Unmarshal(req.THINKING, &thinking))
	assert.Equal(t, "enabled", thinking["type"])
}

func TestApplyDeepSeekV4ClaudeThinkingSuffix_FromChannelStripsOnly(t *testing.T) {
	channelEffort := json.RawMessage(`{"effort":"high"}`)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "deepseek-v4-pro-max",
		},
		ReasoningEffort:            "high",
		ReasoningEffortFromChannel: true,
	}
	req := &dto.ClaudeRequest{
		Model:        "deepseek-v4-pro-max",
		OutputConfig: channelEffort,
	}

	err := applyDeepSeekV4ClaudeThinkingSuffix(info, req)
	require.NoError(t, err)

	assert.Equal(t, "deepseek-v4-pro", req.Model)
	assert.Equal(t, "deepseek-v4-pro", info.UpstreamModelName)
	assert.Equal(t, "high", info.ReasoningEffort)
	assert.JSONEq(t, `{"effort":"high"}`, string(req.OutputConfig))
	assert.Nil(t, req.Thinking)
}

func TestApplyDeepSeekV4ResponsesThinkingSuffix_FromChannelStripsOnly(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "deepseek-v4-pro-max",
		},
		ReasoningEffort:            "high",
		ReasoningEffortFromChannel: true,
	}
	req := &dto.OpenAIResponsesRequest{
		Model: "deepseek-v4-pro-max",
		Reasoning: &dto.Reasoning{
			Effort: "high",
		},
	}

	applyDeepSeekV4ResponsesThinkingSuffix(info, req)

	assert.Equal(t, "deepseek-v4-pro", req.Model)
	assert.Equal(t, "deepseek-v4-pro", info.UpstreamModelName)
	require.NotNil(t, req.Reasoning)
	assert.Equal(t, "high", req.Reasoning.Effort)
	assert.Equal(t, "high", info.ReasoningEffort)
}
