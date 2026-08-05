package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldChatCompletionsUseResponses_ChannelForce(t *testing.T) {
	// Ensure global policy is off for this test.
	orig := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: false,
	}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = orig
	})

	assert.True(t, ShouldChatCompletionsUseResponses(1, constant.ChannelTypeSub2API, "gpt-5", true))
	assert.False(t, ShouldChatCompletionsUseResponses(1, constant.ChannelTypeSub2API, "gpt-5", false))
}

func TestShouldChatCompletionsUseResponses_SkipChannelType(t *testing.T) {
	// Register a throwaway type so the skip registry is exercised without
	// depending on side-effect imports of specific channel packages.
	const skipType = 999001
	common.RegisterSkipChatCompletionsToResponses(skipType)
	require.True(t, common.ShouldSkipChatCompletionsToResponses(skipType))
	assert.False(t, ShouldChatCompletionsUseResponses(1, skipType, "any-model", true))
}

func TestShouldChatCompletionsUseResponses_GlobalStillWorks(t *testing.T) {
	orig := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-5"},
	}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = orig
	})

	assert.True(t, ShouldChatCompletionsUseResponses(1, constant.ChannelTypeOpenAI, "gpt-5.1", false))
	assert.False(t, ShouldChatCompletionsUseResponses(1, constant.ChannelTypeOpenAI, "claude-4", false))
}
