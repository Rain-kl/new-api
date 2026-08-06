package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldChatCompletionsUseResponsesGlobal_SkipChannelType(t *testing.T) {
	const skipType = 999001
	common.RegisterSkipChatCompletionsToResponses(skipType)
	require.True(t, common.ShouldSkipChatCompletionsToResponses(skipType))
	assert.False(t, ShouldChatCompletionsUseResponsesGlobal(1, skipType, "any-model"))
}

func TestShouldChatCompletionsUseResponsesGlobal_GlobalPolicy(t *testing.T) {
	orig := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-5"},
	}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = orig
	})

	assert.True(t, ShouldChatCompletionsUseResponsesGlobal(1, constant.ChannelTypeOpenAI, "gpt-5.1"))
	assert.False(t, ShouldChatCompletionsUseResponsesGlobal(1, constant.ChannelTypeOpenAI, "claude-4"))
}
