package common

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// ResolveChannelReasoningEffort looks up a channel reasoning-effort rule for originModel.
// Matching uses exact equality on trimmed OriginModelName and rule.Model.
// When force is false and clientSpecified is true, apply is false (skip).
func ResolveChannelReasoningEffort(settings dto.ChannelSettings, originModel string, clientSpecified bool) (effort string, force bool, apply bool) {
	if !settings.ReasoningEffortRulesEnabled {
		return "", false, false
	}
	originModel = strings.TrimSpace(originModel)
	if originModel == "" {
		return "", false, false
	}
	var matched *dto.ReasoningEffortRule
	for i := range settings.ReasoningEffortRules {
		rule := &settings.ReasoningEffortRules[i]
		if strings.TrimSpace(rule.Model) == originModel {
			matched = rule
			break
		}
	}
	if matched == nil {
		return "", false, false
	}
	effort = strings.TrimSpace(matched.Effort)
	if effort == "" {
		return "", false, false
	}
	force = matched.Force
	if force {
		return effort, true, true
	}
	if clientSpecified {
		return effort, false, false
	}
	return effort, false, true
}

// ApplyChannelReasoningEffortChat applies channel reasoning-effort rules to a chat completions request.
func ApplyChannelReasoningEffortChat(info *RelayInfo, request *dto.GeneralOpenAIRequest) {
	if info == nil || request == nil {
		return
	}
	settings := dto.ChannelSettings{}
	if info.ChannelMeta != nil {
		settings = info.ChannelSetting
	}
	clientSpecified := strings.TrimSpace(request.ReasoningEffort) != ""
	effort, _, apply := ResolveChannelReasoningEffort(settings, info.OriginModelName, clientSpecified)
	if !apply {
		return
	}
	request.ReasoningEffort = effort
	info.SetReasoningEffort(effort)
	info.ReasoningEffortFromChannel = true
}

// ApplyChannelReasoningEffortResponses applies channel reasoning-effort rules to a Responses API request.
func ApplyChannelReasoningEffortResponses(info *RelayInfo, request *dto.OpenAIResponsesRequest) {
	if info == nil || request == nil {
		return
	}
	settings := dto.ChannelSettings{}
	if info.ChannelMeta != nil {
		settings = info.ChannelSetting
	}
	clientSpecified := request.Reasoning != nil && strings.TrimSpace(request.Reasoning.Effort) != ""
	effort, _, apply := ResolveChannelReasoningEffort(settings, info.OriginModelName, clientSpecified)
	if !apply {
		return
	}
	if request.Reasoning == nil {
		request.Reasoning = &dto.Reasoning{}
	}
	request.Reasoning.Effort = effort
	info.SetReasoningEffort(effort)
	info.ReasoningEffortFromChannel = true
}

// ApplyChannelReasoningEffortClaude applies channel reasoning-effort rules to an Anthropic Messages request.
// Client is considered specified when output_config.effort is set or Thinking.Type is non-empty.
// Does not auto-create thinking objects.
func ApplyChannelReasoningEffortClaude(info *RelayInfo, request *dto.ClaudeRequest) {
	if info == nil || request == nil {
		return
	}
	settings := dto.ChannelSettings{}
	if info.ChannelMeta != nil {
		settings = info.ChannelSetting
	}
	clientSpecified := strings.TrimSpace(request.GetEfforts()) != "" ||
		(request.Thinking != nil && strings.TrimSpace(request.Thinking.Type) != "")
	effort, _, apply := ResolveChannelReasoningEffort(settings, info.OriginModelName, clientSpecified)
	if !apply {
		return
	}
	merged, err := mergeClaudeOutputConfigEffort(request.OutputConfig, effort)
	if err != nil {
		return
	}
	request.OutputConfig = merged
	info.SetReasoningEffort(effort)
	info.ReasoningEffortFromChannel = true
}

// mergeClaudeOutputConfigEffort merges effort into output_config JSON without dropping other keys.
// Empty or invalid OutputConfig is treated as an empty map.
func mergeClaudeOutputConfigEffort(outputConfig json.RawMessage, effort string) (json.RawMessage, error) {
	cfg := make(map[string]any)
	if len(outputConfig) > 0 {
		_ = common.Unmarshal(outputConfig, &cfg)
		if cfg == nil {
			cfg = make(map[string]any)
		}
	}
	cfg["effort"] = effort
	return common.Marshal(cfg)
}
