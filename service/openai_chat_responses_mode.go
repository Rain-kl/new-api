package service

import (
	"regexp"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

// Chat→Responses upgrade policy is host routing logic (it decides *whether*
// to convert, reading host settings), so it lives here, not in relayconvert.

var chatResponsesRegexCache sync.Map // map[string]*regexp.Regexp

func matchAnyModelPattern(patterns []string, model string) bool {
	if len(patterns) == 0 || model == "" {
		return false
	}
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		re, ok := chatResponsesRegexCache.Load(pattern)
		if !ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				// Treat invalid patterns as non-matching to avoid breaking runtime traffic.
				continue
			}
			re = compiled
			chatResponsesRegexCache.Store(pattern, re)
		}
		if re.(*regexp.Regexp).MatchString(model) {
			return true
		}
	}
	return false
}

func ShouldChatCompletionsUseResponsesPolicy(policy model_setting.ChatCompletionsToResponsesPolicy, channelID int, channelType int, model string) bool {
	// Personal channels (e.g. Echo) register skip via common.RegisterSkipChatCompletionsToResponses.
	if common.ShouldSkipChatCompletionsToResponses(channelType) {
		return false
	}
	if !policy.IsChannelEnabled(channelID, channelType) {
		return false
	}
	return matchAnyModelPattern(policy.ModelPatterns, model)
}

func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	return ShouldChatCompletionsUseResponsesPolicy(
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy,
		channelID,
		channelType,
		model,
	)
}

// ShouldChatCompletionsUseResponses reports whether chat completions should be
// upgraded to the Responses API for this request.
//
// Order:
//  1. Channel types that registered a skip (Echo, etc.) never convert.
//  2. Channel setting chat_completions_to_responses forces conversion for all models.
//  3. Otherwise fall back to the global policy (channel allowlist + model patterns).
func ShouldChatCompletionsUseResponses(channelID int, channelType int, model string, channelForce bool) bool {
	if common.ShouldSkipChatCompletionsToResponses(channelType) {
		return false
	}
	if channelForce {
		return true
	}
	return ShouldChatCompletionsUseResponsesGlobal(channelID, channelType, model)
}
