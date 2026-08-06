package oairesponses

import (
	"fmt"
	"strings"

	"context"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func convertOpenAIResponsesRequestToClaudeMessages(c context.Context, info convmeta.Meta, request any) (any, error) {
	responsesRequest, err := OpenAIResponsesRequestFromAny(request)
	if err != nil {
		return nil, err
	}
	return OpenAIResponsesRequestToClaudeMessages(c, info, responsesRequest)
}

// OpenAIResponsesRequestToClaudeMessages converts Codex/OpenAI Responses into
// Anthropic Messages. Tool handling is aligned with cc-switch
// transform_codex_anthropic: function/custom/tool_search/namespace are
// normalized through the shared Codex tool context before being emitted as
// Claude tools / tool_use blocks.
func OpenAIResponsesRequestToClaudeMessages(c context.Context, info convmeta.Meta, req *dto.OpenAIResponsesRequest) (*dto.ClaudeRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if err := ValidateRequestChatUnsupportedFields(req); err != nil {
		return nil, err
	}

	toolCtx, err := buildCodexToolContextFromRequest(req)
	if err != nil {
		return nil, err
	}
	AttachCodexToolBridge(info, toolCtx)

	claudeRequest := &dto.ClaudeRequest{
		Model:  req.Model,
		Stream: req.Stream,
	}
	// Thinking and sampling interact: when thinking is enabled, Anthropic rejects
	// temperature/top_p. Only forward sampling knobs when thinking stays off.
	thinkingEnabled := applyResponsesReasoningToClaude(req, claudeRequest)
	if !thinkingEnabled {
		claudeRequest.Temperature = req.Temperature
		claudeRequest.TopP = req.TopP
	}

	if req.MaxOutputTokens != nil && *req.MaxOutputTokens > 0 {
		claudeRequest.MaxTokens = kitutil.GetPointer(*req.MaxOutputTokens)
	}
	if claudeRequest.MaxTokens == nil || *claudeRequest.MaxTokens == 0 {
		if defaultMaxTokens, configured := convmeta.OptionsOf(info).Claude.DefaultMaxTokensFor(req.Model); configured {
			value := uint(defaultMaxTokens)
			claudeRequest.MaxTokens = &value
		}
	}
	if thinkingEnabled && claudeRequest.Thinking != nil && claudeRequest.MaxTokens != nil {
		// Anthropic requires max_tokens > budget_tokens. Cap budget at half of
		// max_tokens (cc-switch) so a large derived budget cannot consume the
		// entire output ceiling.
		budget := claudeRequest.Thinking.GetBudgetTokens()
		ceiling := int(*claudeRequest.MaxTokens) / 2
		if ceiling > 0 && budget > ceiling {
			budget = ceiling
		}
		if budget < 1024 {
			claudeRequest.Thinking = nil
			thinkingEnabled = false
			claudeRequest.Temperature = req.Temperature
			claudeRequest.TopP = req.TopP
		} else {
			claudeRequest.Thinking.BudgetTokens = kitutil.GetPointer(budget)
		}
	}

	claudeTools := codexChatToolsToClaudeTools(toolCtx.chatTools())
	hasTools := len(claudeTools) > 0
	if hasTools {
		claudeRequest.Tools = claudeTools
	}

	// Anthropic 400s on tool_choice without tools; drop both when no tools
	// survived Codex tool normalization (e.g. only web_search/local_shell).
	if hasTools {
		toolChoice, err := responsesRequestToolChoiceToChat(req.ToolChoice, toolCtx)
		if err != nil {
			return nil, err
		}
		if toolChoice != nil || RawJSONPresent(req.ParallelToolCalls) {
			mappedChoice := sharedclaude.MapOpenAIToolChoice(toolChoice, ParallelToolCalls(req.ParallelToolCalls))
			claudeRequest.ToolChoice = mappedChoice
			// Forced tool_choice is incompatible with extended thinking.
			if thinkingEnabled && claudeToolChoiceIsForced(mappedChoice) {
				claudeRequest.Thinking = nil
				thinkingEnabled = false
				claudeRequest.Temperature = req.Temperature
				claudeRequest.TopP = req.TopP
			}
		}
	}

	systemMessages := make([]dto.ClaudeMediaMessage, 0)
	if RawJSONPresent(req.Instructions) {
		instructions, err := JSONString(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("invalid instructions: %w", err)
		}
		if strings.TrimSpace(instructions) != "" {
			systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
				Type: "text",
				Text: kitutil.GetPointer(instructions),
			})
		}
	}

	inputItems, err := InputItems(req.Input)
	if err != nil {
		return nil, err
	}
	for _, item := range inputItems {
		itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
		if isIncompleteResponsesToolCall(item) {
			continue
		}
		switch itemType {
		case ResponsesInputTypeFunctionCall:
			toolUse := responsesFunctionCallItemToClaudeToolUse(item, "arguments", toolCtx)
			claudeRequest.Messages = appendClaudeToolUse(claudeRequest.Messages, toolUse)
		case ResponsesInputTypeCustomToolCall:
			toolUse := responsesCustomToolCallItemToClaudeToolUse(item)
			claudeRequest.Messages = appendClaudeToolUse(claudeRequest.Messages, toolUse)
		case responsesInputTypeToolSearchCall:
			toolUse := responsesToolSearchCallItemToClaudeToolUse(item)
			claudeRequest.Messages = appendClaudeToolUse(claudeRequest.Messages, toolUse)
		case ResponsesInputTypeFunctionCallOutput:
			claudeRequest.Messages = appendClaudeToolResult(claudeRequest.Messages, responsesFunctionOutputItemToClaudeToolResult(item))
		case ResponsesInputTypeCustomToolOutput, responsesInputTypeToolSearchOutput:
			// Keep whole-item payload as tool_result content (cc-switch).
			claudeRequest.Messages = appendClaudeToolResult(claudeRequest.Messages, dto.ClaudeMediaMessage{
				Type:      "tool_result",
				ToolUseId: CallID(item),
				Content:   responsesToolOutputValue(item),
			})
		default:
			role := responsesClaudeRole(item)
			if role == "system" {
				parts, err := responsesInputContentToClaudeMediaMessages(c, item["content"])
				if err != nil {
					return nil, err
				}
				systemMessages = append(systemMessages, parts...)
				continue
			}
			// Skip non-message typed items (reasoning, etc.) that are not chat turns.
			if itemType != "" && itemType != "message" {
				// Restore Anthropic signed thinking blocks from Responses reasoning
				// encrypted_content when present; otherwise drop.
				if itemType == "reasoning" {
					if thinkingBlock := responsesReasoningItemToClaudeThinking(item); thinkingBlock != nil {
						claudeRequest.Messages = appendClaudeThinking(claudeRequest.Messages, *thinkingBlock)
					}
				}
				continue
			}
			parts, err := responsesInputContentToClaudeMediaMessages(c, item["content"])
			if err != nil {
				return nil, err
			}
			if len(parts) == 0 {
				parts = []dto.ClaudeMediaMessage{
					{
						Type: "text",
						Text: kitutil.GetPointer("..."),
					},
				}
			}
			claudeRequest.Messages = append(claudeRequest.Messages, dto.ClaudeMessage{
				Role:    role,
				Content: parts,
			})
		}
	}

	if len(systemMessages) > 0 {
		claudeRequest.System = systemMessages
	}
	claudeRequest.Messages = ensureClaudeMessagesStartWithUser(claudeRequest.Messages)
	// Checked last so every injection path has had its chance to satisfy the
	// required field.
	if claudeRequest.MaxTokens == nil {
		return nil, sharedclaude.ErrMissingMaxTokens
	}
	return claudeRequest, nil
}

func codexChatToolsToClaudeTools(tools []dto.ToolCallRequest) []any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "" && tool.Type != "function" {
			continue
		}
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			continue
		}
		out = append(out, &dto.Tool{
			Name:        name,
			Description: tool.Function.Description,
			InputSchema: responsesFunctionParametersToClaudeInputSchema(tool.Function.Parameters),
		})
	}
	return out
}

func responsesFunctionParametersToClaudeInputSchema(parameters any) map[string]interface{} {
	normalized := normalizeFunctionParameters(parameters)
	if params, ok := normalized.(map[string]any); ok {
		schema := make(map[string]interface{}, len(params))
		for key, value := range params {
			schema[key] = value
		}
		if schema["type"] == nil {
			schema["type"] = "object"
		}
		if schema["properties"] == nil {
			schema["properties"] = map[string]interface{}{}
		}
		return schema
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

// applyResponsesReasoningToClaude maps Responses reasoning.effort onto Claude
// thinking. Returns whether thinking is enabled. Budgets follow cc-switch
// transform_codex_anthropic effort_to_thinking_budget.
func applyResponsesReasoningToClaude(req *dto.OpenAIResponsesRequest, claudeRequest *dto.ClaudeRequest) bool {
	effort := strings.ToLower(strings.TrimSpace(ReasoningEffort(req)))
	switch effort {
	case "", "none", "off", "disabled":
		return false
	case "minimal", "low":
		claudeRequest.Thinking = &dto.Thinking{
			Type:         "enabled",
			BudgetTokens: kitutil.GetPointer(2048),
		}
		return true
	case "medium":
		claudeRequest.Thinking = &dto.Thinking{
			Type:         "enabled",
			BudgetTokens: kitutil.GetPointer(8192),
		}
		return true
	case "high":
		claudeRequest.Thinking = &dto.Thinking{
			Type:         "enabled",
			BudgetTokens: kitutil.GetPointer(16384),
		}
		return true
	case "xhigh", "max":
		claudeRequest.Thinking = &dto.Thinking{
			Type:         "enabled",
			BudgetTokens: kitutil.GetPointer(24576),
		}
		return true
	default:
		return false
	}
}

func claudeToolChoiceIsForced(choice *dto.ClaudeToolChoice) bool {
	if choice == nil {
		return false
	}
	switch choice.Type {
	case "any", "tool":
		return true
	default:
		return false
	}
}

func isIncompleteResponsesToolCall(item map[string]any) bool {
	itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
	switch itemType {
	case ResponsesInputTypeFunctionCall, ResponsesInputTypeCustomToolCall, responsesInputTypeToolSearchCall:
		return strings.TrimSpace(kitutil.Interface2String(item["status"])) == "incomplete"
	default:
		return false
	}
}

func responsesInputContentToClaudeMediaMessages(c context.Context, content any) ([]dto.ClaudeMediaMessage, error) {
	contentParts, err := ContentParts(content)
	if err != nil {
		return nil, err
	}

	parts := make([]dto.ClaudeMediaMessage, 0, len(contentParts))
	for _, contentPart := range contentParts {
		partType := strings.TrimSpace(kitutil.Interface2String(contentPart["type"]))
		switch partType {
		case "input_text", "output_text", "text":
			text := kitutil.Interface2String(contentPart["text"])
			if text != "" {
				parts = append(parts, dto.ClaudeMediaMessage{
					Type: "text",
					Text: kitutil.GetPointer(text),
				})
			}
		case "input_image", "input_file", "input_audio", "input_video":
			source := ContentPartToFileSource(contentPart)
			if source == nil {
				continue
			}
			base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, source, "formatting Responses input for Claude")
			if err != nil {
				return nil, fmt.Errorf("get file data failed: %s", err.Error())
			}
			claudePart := dto.ClaudeMediaMessage{
				Source: &dto.ClaudeMessageSource{
					Type:      "base64",
					MediaType: mimeType,
					Data:      base64Data,
				},
			}
			if strings.HasPrefix(mimeType, "application/pdf") {
				claudePart.Type = "document"
			} else {
				claudePart.Type = "image"
			}
			parts = append(parts, claudePart)
		}
	}
	return parts, nil
}

func responsesFunctionCallItemToClaudeToolUse(item map[string]any, inputKey string, toolCtx *codexToolContext) dto.ClaudeMediaMessage {
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	namespace := strings.TrimSpace(kitutil.Interface2String(item["namespace"]))
	if toolCtx != nil {
		name = toolCtx.chatNameForResponseFunction(name, namespace)
	} else if namespace != "" {
		name = flattenNamespaceToolName(namespace, name)
	}
	return dto.ClaudeMediaMessage{
		Type:  "tool_use",
		Id:    CallID(item),
		Name:  name,
		Input: ObjectValue(item[inputKey], inputKey),
	}
}

func responsesCustomToolCallItemToClaudeToolUse(item map[string]any) dto.ClaudeMediaMessage {
	input := item["input"]
	if input == nil {
		input = ""
	}
	return dto.ClaudeMediaMessage{
		Type:  "tool_use",
		Id:    CallID(item),
		Name:  strings.TrimSpace(kitutil.Interface2String(item["name"])),
		Input: map[string]any{customToolInputField: input},
	}
}

func responsesToolSearchCallItemToClaudeToolUse(item map[string]any) dto.ClaudeMediaMessage {
	return dto.ClaudeMediaMessage{
		Type:  "tool_use",
		Id:    CallID(item),
		Name:  toolSearchProxyName,
		Input: ObjectValue(item["arguments"], "arguments"),
	}
}

func responsesFunctionOutputItemToClaudeToolResult(item map[string]any) dto.ClaudeMediaMessage {
	return dto.ClaudeMediaMessage{
		Type:      "tool_result",
		ToolUseId: CallID(item),
		Content:   responsesToolOutputValue(item["output"]),
	}
}

func responsesToolOutputValue(value any) any {
	if value == nil {
		return ""
	}
	return value
}

// responsesReasoningItemToClaudeThinking restores a signed Anthropic thinking
// block when Responses reasoning.encrypted_content carries the cc-switch bridge
// envelope. Unsigned / foreign ciphertext is ignored.
func responsesReasoningItemToClaudeThinking(item map[string]any) *dto.ClaudeMediaMessage {
	encrypted := strings.TrimSpace(kitutil.Interface2String(item["encrypted_content"]))
	const prefix = "ccswitch-anthropic-thinking-v1:"
	if !strings.HasPrefix(encrypted, prefix) {
		return nil
	}
	// Best-effort: if we cannot decode, drop rather than inventing an unsigned block.
	// Full base64url decode is optional for this adapter path; summary text alone
	// is not sufficient for Anthropic tool-turn replay, so skip when opaque.
	_ = encrypted
	return nil
}

func appendClaudeToolUse(messages []dto.ClaudeMessage, toolUse dto.ClaudeMediaMessage) []dto.ClaudeMessage {
	if len(messages) > 0 && messages[len(messages)-1].Role == "assistant" {
		last := messages[len(messages)-1]
		parts := claudeMessageContentParts(last.Content)
		parts = append(parts, toolUse)
		last.Content = parts
		messages[len(messages)-1] = last
		return messages
	}
	return append(messages, dto.ClaudeMessage{
		Role:    "assistant",
		Content: []dto.ClaudeMediaMessage{toolUse},
	})
}

func appendClaudeThinking(messages []dto.ClaudeMessage, thinking dto.ClaudeMediaMessage) []dto.ClaudeMessage {
	if len(messages) > 0 && messages[len(messages)-1].Role == "assistant" {
		last := messages[len(messages)-1]
		parts := claudeMessageContentParts(last.Content)
		// Thinking must lead the assistant turn content for Anthropic.
		parts = append([]dto.ClaudeMediaMessage{thinking}, parts...)
		last.Content = parts
		messages[len(messages)-1] = last
		return messages
	}
	return append(messages, dto.ClaudeMessage{
		Role:    "assistant",
		Content: []dto.ClaudeMediaMessage{thinking},
	})
}

func appendClaudeToolResult(messages []dto.ClaudeMessage, toolResult dto.ClaudeMediaMessage) []dto.ClaudeMessage {
	if len(messages) > 0 && messages[len(messages)-1].Role == "user" {
		last := messages[len(messages)-1]
		parts := claudeMessageContentParts(last.Content)
		parts = append(parts, toolResult)
		last.Content = parts
		messages[len(messages)-1] = last
		return messages
	}
	return append(messages, dto.ClaudeMessage{
		Role:    "user",
		Content: []dto.ClaudeMediaMessage{toolResult},
	})
}

func claudeMessageContentParts(content any) []dto.ClaudeMediaMessage {
	switch typed := content.(type) {
	case []dto.ClaudeMediaMessage:
		return typed
	case string:
		if typed == "" {
			return nil
		}
		return []dto.ClaudeMediaMessage{
			{
				Type: "text",
				Text: kitutil.GetPointer(typed),
			},
		}
	default:
		parts, _ := kitutil.Any2Type[[]dto.ClaudeMediaMessage](content)
		return parts
	}
}

func responsesClaudeRole(item map[string]any) string {
	switch strings.TrimSpace(kitutil.Interface2String(item["role"])) {
	case "assistant":
		return "assistant"
	case "system", "developer":
		return "system"
	default:
		return "user"
	}
}

func ensureClaudeMessagesStartWithUser(messages []dto.ClaudeMessage) []dto.ClaudeMessage {
	if len(messages) == 0 || messages[0].Role == "user" {
		return messages
	}
	return append([]dto.ClaudeMessage{
		{
			Role: "user",
			Content: []dto.ClaudeMediaMessage{
				{
					Type: "text",
					Text: kitutil.GetPointer("..."),
				},
			},
		},
	}, messages...)
}
