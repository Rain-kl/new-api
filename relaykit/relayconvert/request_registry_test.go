package relayconvert

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeMessagesToOpenAIChatPreservesAssistantTextWithToolCalls(t *testing.T) {
	text := "I will look that up."
	request := dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "assistant",
				Content: []dto.ClaudeMediaMessage{
					{Type: "text", Text: &text},
					{
						Type:  "tool_use",
						Id:    "call_1",
						Name:  "lookup",
						Input: map[string]any{"query": "weather"},
					},
				},
			},
		},
	}

	result, err := ConvertRequest(
		context.Background(),
		&convmeta.Values{},
		types.RelayFormatOpenAI,
		&request,
	)
	require.NoError(t, err)

	converted, ok := result.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, converted.Messages, 1)

	assistant := converted.Messages[0]
	assert.Equal(t, "assistant", assistant.Role)
	toolCalls := assistant.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_1", toolCalls[0].ID)
	require.Len(t, assistant.ParseContent(), 1)
	assert.Equal(t, "text", assistant.ParseContent()[0].Type)
	assert.Equal(t, text, assistant.ParseContent()[0].Text)
}

func TestClaudeMessagesToOpenAIChatKeepsToolOnlyContentEmpty(t *testing.T) {
	request := dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{
				Role: "assistant",
				Content: []dto.ClaudeMediaMessage{
					{
						Type:  "tool_use",
						Id:    "call_1",
						Name:  "lookup",
						Input: map[string]any{"query": "weather"},
					},
				},
			},
		},
	}

	result, err := ConvertRequest(
		context.Background(),
		&convmeta.Values{},
		types.RelayFormatOpenAI,
		&request,
	)
	require.NoError(t, err)

	converted, ok := result.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, converted.Messages, 1)

	assistant := converted.Messages[0]
	assert.Empty(t, assistant.ParseContent())
	toolCalls := assistant.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_1", toolCalls[0].ID)
}

func TestRequestConverterRegistryListsSupportedTextConverters(t *testing.T) {
	tests := []struct {
		converter      string
		from           types.RelayFormat
		to             types.RelayFormat
		quality        RequestConverterQuality
		stepConverters []string
		advancedCustom bool
	}{
		{converter: ConverterClaudeMessagesToOpenAIChat, from: types.RelayFormatClaude, to: types.RelayFormatOpenAI, quality: RequestConverterQualityFair, advancedCustom: true},
		{converter: ConverterGeminiContentToOpenAIChat, from: types.RelayFormatGemini, to: types.RelayFormatOpenAI, quality: RequestConverterQualityFair, advancedCustom: true},
		{converter: ConverterOpenAIChatToClaudeMessages, from: types.RelayFormatOpenAI, to: types.RelayFormatClaude, quality: RequestConverterQualityFair, advancedCustom: true},
		{converter: ConverterOpenAIChatToGeminiContent, from: types.RelayFormatOpenAI, to: types.RelayFormatGemini, quality: RequestConverterQualityFair, advancedCustom: true},
		{converter: ConverterOpenAIChatToOpenAIResponses, from: types.RelayFormatOpenAI, to: types.RelayFormatOpenAIResponses, quality: RequestConverterQualityGood, advancedCustom: true},
		{converter: ConverterOpenAIResponsesToOpenAIChat, from: types.RelayFormatOpenAIResponses, to: types.RelayFormatOpenAI, quality: RequestConverterQualityGood, advancedCustom: true},
		{
			converter: requestConverterClaudeToGemini,
			from:      types.RelayFormatClaude,
			to:        types.RelayFormatGemini,
			quality:   RequestConverterQualityDiscouraged,
			stepConverters: []string{
				ConverterClaudeMessagesToOpenAIChat,
				ConverterOpenAIChatToGeminiContent,
			},
		},
		{
			converter:      requestConverterClaudeToResponses,
			from:           types.RelayFormatClaude,
			to:             types.RelayFormatOpenAIResponses,
			quality:        RequestConverterQualityFair,
			advancedCustom: true,
		},
		{
			converter: requestConverterGeminiToClaude,
			from:      types.RelayFormatGemini,
			to:        types.RelayFormatClaude,
			quality:   RequestConverterQualityDiscouraged,
			stepConverters: []string{
				ConverterGeminiContentToOpenAIChat,
				ConverterOpenAIChatToClaudeMessages,
			},
		},
		{
			converter: requestConverterGeminiToResponses,
			from:      types.RelayFormatGemini,
			to:        types.RelayFormatOpenAIResponses,
			quality:   RequestConverterQualityFair,
			stepConverters: []string{
				ConverterGeminiContentToOpenAIChat,
				ConverterOpenAIChatToOpenAIResponses,
			},
			advancedCustom: true,
		},
		{
			converter:      ConverterOpenAIResponsesToClaudeMessages,
			from:           types.RelayFormatOpenAIResponses,
			to:             types.RelayFormatClaude,
			quality:        RequestConverterQualityFair,
			advancedCustom: true,
		},
		{
			converter:      ConverterOpenAIResponsesToGemini,
			from:           types.RelayFormatOpenAIResponses,
			to:             types.RelayFormatGemini,
			quality:        RequestConverterQualityFair,
			advancedCustom: true,
		},
	}

	require.Len(t, requestConverters, len(tests))

	for _, tt := range tests {
		t.Run(tt.converter, func(t *testing.T) {
			spec, ok := LookupRequestConverter(tt.converter)

			require.True(t, ok)
			assert.Equal(t, tt.converter, spec.ID)
			assert.Equal(t, tt.from, spec.From)
			assert.Equal(t, tt.to, spec.To)
			assert.Equal(t, tt.quality, spec.Quality)
			assert.Equal(t, tt.stepConverters, spec.StepConverters)
			if len(tt.stepConverters) == 0 {
				assert.NotNil(t, spec.Convert)
			} else {
				assert.Nil(t, spec.Convert)
			}
			assert.Equal(t, tt.advancedCustom, dto.IsAdvancedCustomConverterAllowed(tt.converter))
		})
	}
}

func TestConvertRequestToTargetRecordsConversionChain(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatOpenAI},
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, req)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	assert.Equal(t, types.RelayFormatOpenAI, result.From)
	assert.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), result.To)
	assert.Equal(t, ConverterOpenAIChatToOpenAIResponses, result.Converter)
	assert.Equal(t, RequestConverterQualityGood, result.Quality)
	assert.Equal(t, []RequestStep{
		{
			Converter: ConverterOpenAIChatToOpenAIResponses,
			From:      types.RelayFormatOpenAI,
			To:        types.RelayFormatOpenAIResponses,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

func TestConvertRequestClaudeToResponsesUsesDirectPath(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatClaude},
	}
	req := &dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, req)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	assert.Equal(t, types.RelayFormat(types.RelayFormatClaude), result.From)
	assert.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), result.To)
	assert.Equal(t, requestConverterClaudeToResponses, result.Converter)
	assert.Equal(t, RequestConverterQualityFair, result.Quality)
	assert.Equal(t, []RequestStep{
		{
			Converter: requestConverterClaudeToResponses,
			From:      types.RelayFormatClaude,
			To:        types.RelayFormatOpenAIResponses,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

func TestConvertRequestClaudeToChatResolvesToolResultNames(t *testing.T) {
	req := &dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_result", ToolUseId: "call_1", Content: "before call"},
			}},
			{Role: "assistant", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_use", Id: "call_1", Name: "first", Input: map[string]any{}},
			}},
			{Role: "user", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_result", ToolUseId: "call_1", Content: "after call"},
				{Type: "tool_result", ToolUseId: "missing", Content: "unknown"},
				{Type: "tool_result", ToolUseId: "call_1", Name: "explicit", Content: "named"},
			}},
			{Role: "assistant", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_use", Id: "call_1", Name: "later", Input: map[string]any{}},
			}},
		},
	}

	result, err := ConvertRequestByID(nil, nil, ConverterClaudeMessagesToOpenAIChat, req)
	require.NoError(t, err)
	chatReq, ok := result.Value.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Len(t, chatReq.Messages, 6)
	for _, tt := range []struct {
		index int
		id    string
		name  string
	}{
		{0, "call_1", "first"},
		{2, "call_1", "first"},
		{3, "missing", ""},
		{4, "call_1", "explicit"},
	} {
		message := chatReq.Messages[tt.index]
		assert.Equal(t, "tool", message.Role)
		assert.Equal(t, tt.id, message.ToolCallId)
		require.NotNil(t, message.Name)
		assert.Equal(t, tt.name, *message.Name)
	}
	assert.Equal(t, "assistant", chatReq.Messages[1].Role)
	assert.Equal(t, "assistant", chatReq.Messages[5].Role)
}

func TestConvertRequestClaudeToResponsesPreservesMixedBlockOrder(t *testing.T) {
	info := &convmeta.Values{ConversionChain: []types.RelayFormat{types.RelayFormatClaude}}
	stream := true
	strict := true
	maxTokens := uint(4096)
	req := &dto.ClaudeRequest{
		Model:     "gpt-test",
		System:    []dto.ClaudeMediaMessage{{Type: "text", Text: kitutil.GetPointer("system ")}, {Type: "text", Text: kitutil.GetPointer("rules")}},
		MaxTokens: &maxTokens,
		Stream:    &stream,
		Tools: []dto.Tool{{
			Name:        "lookup",
			Description: "Look up a value",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
			Strict:      &strict,
		}},
		ToolChoice: dto.ClaudeToolChoice{Type: "tool", Name: "lookup", DisableParallelToolUse: true},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "text", Text: kitutil.GetPointer("question")}}},
			{Role: "assistant", Content: []dto.ClaudeMediaMessage{
				{Type: "text", Text: kitutil.GetPointer("before")},
				{Type: "tool_use", Id: "call_1", Name: "lookup", Input: map[string]any{"q": "x"}},
				{Type: "text", Text: kitutil.GetPointer("after")},
			}},
			{Role: "user", Content: []dto.ClaudeMediaMessage{
				{Type: "tool_result", ToolUseId: "call_1", Content: "result"},
				{Type: "text", Text: kitutil.GetPointer("continue")},
			}},
		},
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)
	responsesReq := result.Value.(*dto.OpenAIResponsesRequest)
	assert.Equal(t, "gpt-test", responsesReq.Model)
	assert.Equal(t, maxTokens, *responsesReq.MaxOutputTokens)
	assert.True(t, *responsesReq.Stream)
	assert.JSONEq(t, `"system rules"`, string(responsesReq.Instructions))
	assert.JSONEq(t, `[{"type":"function","name":"lookup","description":"Look up a value","parameters":{"type":"object","properties":{"q":{"type":"string"}}},"strict":true}]`, string(responsesReq.Tools))
	assert.JSONEq(t, `{"type":"function","name":"lookup"}`, string(responsesReq.ToolChoice))
	assert.JSONEq(t, `false`, string(responsesReq.ParallelToolCalls))

	var input []map[string]any
	require.NoError(t, kitutil.Unmarshal(responsesReq.Input, &input))
	require.Len(t, input, 6)
	assert.Equal(t, "user", input[0]["role"])
	assert.Equal(t, "question", inputContentText(t, input[0]))
	assert.Equal(t, "assistant", input[1]["role"])
	assert.Equal(t, "before", inputContentText(t, input[1]))
	assert.Equal(t, "function_call", input[2]["type"])
	assert.Equal(t, "call_1", input[2]["call_id"])
	assert.Equal(t, "lookup", input[2]["name"])
	assert.JSONEq(t, `{"q":"x"}`, input[2]["arguments"].(string))
	assert.Equal(t, "assistant", input[3]["role"])
	assert.Equal(t, "after", inputContentText(t, input[3]))
	assert.Equal(t, "function_call_output", input[4]["type"])
	assert.Equal(t, "result", input[4]["output"])
	assert.Equal(t, "user", input[5]["role"])
	assert.Equal(t, "continue", inputContentText(t, input[5]))
}

func TestConvertRequestClaudeToResponsesDefaultsImageDetail(t *testing.T) {
	const imageData = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+Xoy8AAAAASUVORK5CYII="
	req := &dto.ClaudeRequest{
		Model: "gpt-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: []dto.ClaudeMediaMessage{{
				Type:   "image",
				Source: &dto.ClaudeMessageSource{Type: "url", Url: "https://example.test/a.png"},
			}}},
			{Role: "assistant", Content: []dto.ClaudeMediaMessage{{
				Type: "tool_use", Id: "call_1", Name: "screenshot", Input: map[string]any{},
			}}},
			{Role: "user", Content: []dto.ClaudeMediaMessage{{
				Type: "tool_result", ToolUseId: "call_1",
				Content: []dto.ClaudeMediaMessage{{
					Type:   "image",
					Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: imageData},
				}},
			}}},
		},
	}

	result, err := ConvertRequestByID(context.Background(), nil, ConverterClaudeMessagesToOpenAIResponses, req)
	require.NoError(t, err)
	responsesReq, ok := result.Value.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.JSONEq(t, `[
		{"role":"user","content":[
			{"type":"input_image","image_url":"https://example.test/a.png","detail":"auto"}
		]},
		{"type":"function_call","call_id":"call_1","name":"screenshot","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_1","output":[
			{"type":"input_image","image_url":"data:image/png;base64,`+imageData+`","detail":"auto"}
		]}
	]`, string(responsesReq.Input))
}

func TestConvertRequestClaudeToResponsesDropsIncompatibleContextManagement(t *testing.T) {
	req := &dto.ClaudeRequest{
		Model: "gpt-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
		ContextManagement: mustRawMessage(t, map[string]any{
			"edits": []map[string]any{{"type": "clear_tool_uses_20250919"}},
		}),
	}

	result, err := ConvertRequest(nil, nil, types.RelayFormatOpenAIResponses, req)

	require.NoError(t, err)
	responsesReq, ok := result.Value.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.Empty(t, responsesReq.ContextManagement)
}

func TestConvertRequestClaudeAdaptiveThinkingPreservesEffort(t *testing.T) {
	tests := []struct {
		name         string
		outputConfig []byte
		wantEffort   string
	}{
		{name: "adaptive default", wantEffort: "high"},
		{name: "explicit low", outputConfig: mustRawMessage(t, map[string]any{"effort": "low"}), wantEffort: "low"},
		{name: "explicit xhigh", outputConfig: mustRawMessage(t, map[string]any{"effort": "xhigh"}), wantEffort: "xhigh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &convmeta.Values{
				OriginModelName: "gpt-5.6-sol",
				ConversionChain: []types.RelayFormat{types.RelayFormatClaude},
			}
			req := &dto.ClaudeRequest{
				Model:        "gpt-5.6-sol",
				OutputConfig: tt.outputConfig,
				Thinking:     &dto.Thinking{Type: "adaptive", Display: "summarized"},
				Messages: []dto.ClaudeMessage{
					{Role: "user", Content: "hello"},
				},
			}

			result, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, req)

			require.NoError(t, err)
			responsesReq, ok := result.Value.(*dto.OpenAIResponsesRequest)
			require.True(t, ok)
			require.NotNil(t, responsesReq.Reasoning)
			assert.Equal(t, tt.wantEffort, responsesReq.Reasoning.Effort)
			assert.Equal(t, "detailed", responsesReq.Reasoning.Summary)
			assert.Equal(t, tt.wantEffort, info.GetReasoningEffort())
		})
	}
}

func TestGeminiThinkingLevelCaseInsensitiveAcrossPaths(t *testing.T) {
	newRequest := func(level string) *dto.GeminiChatRequest {
		return &dto.GeminiChatRequest{
			Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}}},
			GenerationConfig: dto.GeminiChatGenerationConfig{
				ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: level},
			},
		}
	}

	t.Run("native passthrough records canonical effort without rewriting wire value", func(t *testing.T) {
		info := &convmeta.Values{OriginModelName: "gemini-3.7-flash", UpstreamModelName: "gemini-3.7-flash"}
		req := newRequest(" MEDIUM ")
		require.NoError(t, ApplyGeminiThinkingConfigChecked(req, info))
		assert.Equal(t, "medium", info.GetReasoningEffort())
		assert.Equal(t, " MEDIUM ", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
	})

	t.Run("native passthrough keeps unknown level as sent", func(t *testing.T) {
		info := &convmeta.Values{OriginModelName: "gemini-3.7-flash", UpstreamModelName: "gemini-3.7-flash"}
		req := newRequest("ULTRA")
		require.NoError(t, ApplyGeminiThinkingConfigChecked(req, info))
		assert.Equal(t, "ULTRA", info.GetReasoningEffort())
	})

	t.Run("suffix state canonicalizes uppercase level against normalized effort", func(t *testing.T) {
		info := &convmeta.Values{
			OriginModelName:     "gemini-3.7-flash-thinking-medium",
			UpstreamModelName:   "gemini-3.7-flash",
			ChannelMetaAttached: true,
			ReasoningConversion: &dto.ReasoningConversionState{Mode: "enabled", Effort: "medium"},
		}
		req := newRequest("MEDIUM")
		require.NoError(t, ApplyGeminiThinkingConfigChecked(req, info))
		assert.Equal(t, "medium", info.GetReasoningEffort())
		assert.Equal(t, "medium", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
	})

	t.Run("gemini to openai conversion accepts uppercase level", func(t *testing.T) {
		info := &convmeta.Values{
			OriginModelName:   "gemini-3.7-flash",
			UpstreamModelName: "gemini-3.7-flash",
			ConversionChain:   []types.RelayFormat{types.RelayFormatGemini},
		}
		result, err := ConvertRequest(nil, info, types.RelayFormatOpenAI, newRequest("MEDIUM"))
		require.NoError(t, err)
		openaiReq, ok := result.Value.(*dto.GeneralOpenAIRequest)
		require.True(t, ok)
		assert.Equal(t, "medium", openaiReq.ReasoningEffort)
		assert.Equal(t, "medium", info.GetReasoningEffort())
	})

	t.Run("gemini to openai conversion adjusts unsupported level with a diagnostic", func(t *testing.T) {
		info := &convmeta.Values{
			OriginModelName:   "gemini-3-pro-preview",
			UpstreamModelName: "gemini-3-pro-preview",
			ConversionChain:   []types.RelayFormat{types.RelayFormatGemini},
		}
		result, err := ConvertRequest(nil, info, types.RelayFormatOpenAI, newRequest("MINIMAL"))
		require.NoError(t, err)
		openaiReq, ok := result.Value.(*dto.GeneralOpenAIRequest)
		require.True(t, ok)
		assert.Equal(t, "low", openaiReq.ReasoningEffort)
		assert.Equal(t, "low", info.GetReasoningEffort())
		codes := make([]string, 0, len(result.Diagnostics))
		for _, diagnostic := range result.Diagnostics {
			codes = append(codes, diagnostic.Code)
		}
		assert.Contains(t, codes, "gemini_level_adjusted")
	})
}

func TestConvertRequestViaExecutesExplicitPath(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatOpenAI},
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequestVia(nil, info, req, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	assert.Equal(t, []RequestStep{
		{
			Converter: ConverterOpenAIChatToOpenAIResponses,
			From:      types.RelayFormatOpenAI,
			To:        types.RelayFormatOpenAIResponses,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

// customToolInputs are custom_tool_call inputs that must reach the upstream
// as the raw string, including ones that happen to look like JSON.
var customToolInputs = []struct {
	name  string
	input string
}{
	{"json object", `{"a":1}`},
	{"json array", `[1,2]`},
	{"plain text", "*** Begin Patch\n*** End Patch"},
}

func customToolHistoryRequest(t *testing.T, items ...map[string]any) *dto.OpenAIResponsesRequest {
	t.Helper()
	input := append([]map[string]any{{"role": "user", "content": "apply the patch"}}, items...)
	return &dto.OpenAIResponsesRequest{
		Model:           "model-test",
		MaxOutputTokens: lo.ToPtr(uint(64)),
		Input:           mustRawMessage(t, input),
		Tools:           mustRawMessage(t, []map[string]any{{"type": "custom", "name": "apply_patch"}}),
	}
}

func TestConvertRequestResponsesToClaudeKeepsCustomToolHistory(t *testing.T) {
	for _, tc := range customToolInputs {
		t.Run(tc.name, func(t *testing.T) {
			req := customToolHistoryRequest(t,
				map[string]any{"type": "custom_tool_call", "call_id": "call_custom", "name": "apply_patch", "input": tc.input},
				map[string]any{"type": "custom_tool_call_output", "call_id": "call_custom", "output": "ok"},
			)

			result, err := ConvertRequest(nil, &convmeta.Values{}, types.RelayFormatClaude, req)
			require.NoError(t, err)
			claudeReq, ok := result.Value.(*dto.ClaudeRequest)
			require.True(t, ok)
			require.Len(t, claudeReq.Messages, 3)

			assert.Equal(t, "assistant", claudeReq.Messages[1].Role)
			toolUse, err := claudeReq.Messages[1].ParseContent()
			require.NoError(t, err)
			assert.Equal(t, []dto.ClaudeMediaMessage{{
				Type:  "tool_use",
				Id:    "call_custom",
				Name:  "apply_patch",
				Input: map[string]any{"input": tc.input},
			}}, toolUse)

			assert.Equal(t, "user", claudeReq.Messages[2].Role)
			toolResult, err := claudeReq.Messages[2].ParseContent()
			require.NoError(t, err)
			assert.Equal(t, []dto.ClaudeMediaMessage{{Type: "tool_result", ToolUseId: "call_custom", Content: "ok"}}, toolResult)
		})
	}

	t.Run("missing name", func(t *testing.T) {
		req := customToolHistoryRequest(t, map[string]any{"type": "custom_tool_call", "call_id": "call_custom", "input": "ls"})
		_, err := ConvertRequest(nil, &convmeta.Values{}, types.RelayFormatClaude, req)
		require.ErrorContains(t, err, "custom_tool_call item is missing name")
	})
}

func TestConvertRequestResponsesToGeminiKeepsCustomToolHistory(t *testing.T) {
	info := func() *convmeta.Values {
		return &convmeta.Values{Options: &convmeta.Options{Gemini: convmeta.GeminiOptions{FunctionCallThoughtSignatureEnabled: true}}}
	}
	for _, tc := range customToolInputs {
		t.Run(tc.name, func(t *testing.T) {
			// Older Codex history may answer a custom call with function_call_output.
			req := customToolHistoryRequest(t,
				map[string]any{"type": "custom_tool_call", "call_id": "call_custom", "name": "apply_patch", "input": tc.input},
				map[string]any{"type": "custom_tool_call_output", "call_id": "call_custom", "output": "ok"},
				map[string]any{"type": "function_call_output", "call_id": "call_custom", "output": "legacy"},
			)

			result, err := ConvertRequest(nil, info(), types.RelayFormatGemini, req)
			require.NoError(t, err)
			geminiReq, ok := result.Value.(*dto.GeminiChatRequest)
			require.True(t, ok)
			tools := geminiReq.GetTools()
			require.Len(t, tools, 1)
			functions, err := kitutil.Any2Type[[]dto.FunctionRequest](tools[0].FunctionDeclarations)
			require.NoError(t, err)
			require.Len(t, functions, 1)
			assert.Equal(t, "apply_patch", functions[0].Name)
			require.Len(t, geminiReq.Contents, 3)

			model := geminiReq.Contents[1]
			assert.Equal(t, "model", model.Role)
			require.Len(t, model.Parts, 1)
			assert.Equal(t, &dto.FunctionCall{
				ID:           "call_custom",
				FunctionName: "apply_patch",
				Arguments:    map[string]any{"input": tc.input},
			}, model.Parts[0].FunctionCall)
			var thoughtSignature string
			require.NoError(t, kitutil.Unmarshal(model.Parts[0].ThoughtSignature, &thoughtSignature))
			assert.Equal(t, sharedgemini.ThoughtSignatureBypassValue, thoughtSignature)

			user := geminiReq.Contents[2]
			assert.Equal(t, "user", user.Role)
			require.Len(t, user.Parts, 2)
			for index, output := range []string{"ok", "legacy"} {
				assert.Equal(t, &dto.GeminiFunctionResponse{
					Name:     "apply_patch",
					Response: map[string]any{"content": output},
					ID:       []byte(`"call_custom"`),
				}, user.Parts[index].FunctionResponse)
			}
		})
	}

	t.Run("missing name", func(t *testing.T) {
		req := customToolHistoryRequest(t, map[string]any{"type": "custom_tool_call", "call_id": "call_custom", "input": "ls"})
		_, err := ConvertRequest(nil, info(), types.RelayFormatGemini, req)
		require.ErrorContains(t, err, "custom_tool_call item is missing name")
	})
}

func TestConvertRequestResponsesToGeminiUsesDirectConverter(t *testing.T) {
	info := &convmeta.Values{
		Options:             &convmeta.Options{Gemini: convmeta.GeminiOptions{FunctionCallThoughtSignatureEnabled: true}},
		ConversionChain:     []types.RelayFormat{types.RelayFormatOpenAIResponses},
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-test",
	}
	maxOutputTokens := uint(256)
	req := &dto.OpenAIResponsesRequest{
		Model:           "gemini-test",
		Instructions:    mustRawMessage(t, "system rules"),
		MaxOutputTokens: &maxOutputTokens,
		Input: mustRawMessage(t, []map[string]any{
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "I will call."},
				},
			},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  map[string]any{"ok": true},
			},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{
				"type":        "function",
				"name":        "lookup",
				"description": "Lookup data",
				"parameters": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"propertyNames":        map[string]any{"pattern": "^[a-z]+$"},
					"properties": map[string]any{
						"q": map[string]any{
							"type":             "string",
							"exclusiveMinimum": 0,
						},
						"filters": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type":                 "object",
								"additionalProperties": true,
								"properties": map[string]any{
									"name": map[string]any{"type": "string"},
								},
							},
						},
					},
				},
			},
		}),
		Text: mustRawMessage(t, map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "answer",
				"schema": map[string]any{"type": "object"},
			},
		}),
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatGemini, req)

	require.NoError(t, err)
	geminiReq, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)
	assert.Equal(t, ConverterOpenAIResponsesToGemini, result.Converter)
	assert.Equal(t, []RequestStep{
		{
			Converter: ConverterOpenAIResponsesToGemini,
			From:      types.RelayFormatOpenAIResponses,
			To:        types.RelayFormatGemini,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini}, info.ConversionChain)

	require.NotNil(t, geminiReq.SystemInstructions)
	require.Len(t, geminiReq.SystemInstructions.Parts, 1)
	assert.Equal(t, "system rules", geminiReq.SystemInstructions.Parts[0].Text)
	assert.Equal(t, "application/json", geminiReq.GenerationConfig.ResponseMimeType)
	assert.Equal(t, maxOutputTokens, *geminiReq.GenerationConfig.MaxOutputTokens)

	tools := geminiReq.GetTools()
	require.Len(t, tools, 1)
	functions, err := kitutil.Any2Type[[]dto.FunctionRequest](tools[0].FunctionDeclarations)
	require.NoError(t, err)
	require.Len(t, functions, 1)
	assert.Equal(t, "lookup", functions[0].Name)
	var params map[string]any
	require.NoError(t, kitutil.Unmarshal(functions[0].Parameters, &params))
	assert.Equal(t, "OBJECT", params["type"])
	assert.NotContains(t, params, "additionalProperties")
	assert.NotContains(t, params, "propertyNames")
	properties, ok := params["properties"].(map[string]any)
	require.True(t, ok)
	queryParam, ok := properties["q"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "STRING", queryParam["type"])
	assert.NotContains(t, queryParam, "exclusiveMinimum")
	filterParam, ok := properties["filters"].(map[string]any)
	require.True(t, ok)
	filterItems, ok := filterParam["items"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, filterItems, "additionalProperties")

	require.Len(t, geminiReq.Contents, 2)
	assert.Equal(t, "model", geminiReq.Contents[0].Role)
	require.Len(t, geminiReq.Contents[0].Parts, 2)
	functionCall := geminiReq.Contents[0].Parts[0].FunctionCall
	require.NotNil(t, functionCall)
	assert.Equal(t, "lookup", functionCall.FunctionName)
	assert.Equal(t, map[string]any{"q": "x"}, functionCall.Arguments)
	var thoughtSignature string
	require.NoError(t, kitutil.Unmarshal(geminiReq.Contents[0].Parts[0].ThoughtSignature, &thoughtSignature))
	assert.Equal(t, sharedgemini.ThoughtSignatureBypassValue, thoughtSignature)
	assert.Equal(t, "I will call.", geminiReq.Contents[0].Parts[1].Text)

	assert.Equal(t, "user", geminiReq.Contents[1].Role)
	require.Len(t, geminiReq.Contents[1].Parts, 1)
	functionResponse := geminiReq.Contents[1].Parts[0].FunctionResponse
	require.NotNil(t, functionResponse)
	assert.Equal(t, "lookup", functionResponse.Name)
	assert.Equal(t, true, functionResponse.Response["ok"])
	assert.Empty(t, geminiReq.Contents[1].Parts[0].ThoughtSignature)
}

func TestConvertRequestResponsesToGeminiSkipsThoughtSignatureWhenDisabled(t *testing.T) {
	info := &convmeta.Values{
		Options:             &convmeta.Options{Gemini: convmeta.GeminiOptions{FunctionCallThoughtSignatureEnabled: false}},
		ConversionChain:     []types.RelayFormat{types.RelayFormatOpenAIResponses},
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-test",
	}
	req := &dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}},
		}),
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatGemini, req)

	require.NoError(t, err)
	geminiReq, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, geminiReq.Contents, 1)
	require.Len(t, geminiReq.Contents[0].Parts, 1)
	require.NotNil(t, geminiReq.Contents[0].Parts[0].FunctionCall)
	assert.Empty(t, geminiReq.Contents[0].Parts[0].ThoughtSignature)
}

func TestConvertRequestOpenAIChatToGeminiImageConfig(t *testing.T) {
	tests := []struct {
		name   string
		fields string
		want   string
	}{
		{
			name: "omitted",
		},
		{
			name:   "top level 4K",
			fields: `,"imageConfig":{"imageSize":"4K"}`,
			want:   `{"imageSize":"4K"}`,
		},
		{
			name:   "top level aspect ratio",
			fields: `,"imageConfig":{"imageSize":"4K","aspectRatio":"16:9"}`,
			want:   `{"imageSize":"4K","aspectRatio":"16:9"}`,
		},
		{
			name:   "google extra body",
			fields: `,"extra_body":{"google":{"image_config":{"image_size":"4K","aspect_ratio":"16:9"}}}`,
			want:   `{"imageSize":"4K","aspectRatio":"16:9"}`,
		},
		{
			name:   "google extra body takes precedence",
			fields: `,"imageConfig":{"imageSize":"1K"},"extra_body":{"google":{"image_config":{"image_size":"4K"}}}`,
			want:   `{"imageSize":"4K"}`,
		},
		{
			name:   "empty google image config keeps top level config",
			fields: `,"imageConfig":{"imageSize":"4K"},"extra_body":{"google":{"image_config":{}}}`,
			want:   `{"imageSize":"4K"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"model":"gemini-3.1-flash-image-preview","temperature":0.7,"top_p":1,"messages":[{"role":"user","content":[{"type":"text","text":"Remove the distant car"}]}]` + tt.fields + `}`
			var request dto.GeneralOpenAIRequest
			require.NoError(t, kitutil.Unmarshal([]byte(raw), &request))

			body, err := kitutil.Marshal(request)
			require.NoError(t, err)
			var copiedRequest dto.GeneralOpenAIRequest
			require.NoError(t, kitutil.Unmarshal(body, &copiedRequest))

			result, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, &copiedRequest)
			require.NoError(t, err)
			body, err = kitutil.Marshal(result.Value)
			require.NoError(t, err)
			var upstream map[string]json.RawMessage
			require.NoError(t, kitutil.Unmarshal(body, &upstream))
			var generationConfig map[string]json.RawMessage
			require.NoError(t, kitutil.Unmarshal(upstream["generationConfig"], &generationConfig))

			assert.NotContains(t, upstream, "imageConfig")
			if tt.want == "" {
				assert.NotContains(t, generationConfig, "imageConfig")
				return
			}
			require.Contains(t, generationConfig, "imageConfig")
			assert.JSONEq(t, tt.want, string(generationConfig["imageConfig"]))
		})
	}
}

func TestConvertRequestOpenAIChatToGeminiAddsThoughtSignatureForAdvancedCustom(t *testing.T) {
	assistantMessage := dto.Message{Role: "assistant", Content: ""}
	assistantMessage.SetToolCalls([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: `{"q":"x"}`,
			},
		},
	})
	info := &convmeta.Values{
		Options:             &convmeta.Options{Gemini: convmeta.GeminiOptions{FunctionCallThoughtSignatureEnabled: true}},
		ConversionChain:     []types.RelayFormat{types.RelayFormatOpenAI},
		ChannelMetaAttached: true,
		ChannelType:         58, // advanced-custom in the host
		UpstreamModelName:   "gemini-test",
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "gemini-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hi"},
			assistantMessage,
			{Role: "tool", ToolCallId: "call_1", Content: `{"ok":true}`},
		},
		Tools: []dto.ToolCallRequest{
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:       "lookup",
					Parameters: json.RawMessage(`{"type":"object"}`),
				},
			},
		},
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatGemini, req)

	require.NoError(t, err)
	geminiReq, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, geminiReq.Contents, 3)
	assert.Equal(t, "model", geminiReq.Contents[1].Role)
	require.Len(t, geminiReq.Contents[1].Parts, 1)
	require.NotNil(t, geminiReq.Contents[1].Parts[0].FunctionCall)
	var thoughtSignature string
	require.NoError(t, kitutil.Unmarshal(geminiReq.Contents[1].Parts[0].ThoughtSignature, &thoughtSignature))
	assert.Equal(t, sharedgemini.ThoughtSignatureBypassValue, thoughtSignature)
}

func TestConvertRequestResponsesToClaudeUsesDirectConverter(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatOpenAIResponses},
	}
	stream := true
	parallelToolCalls := false
	maxOutputTokens := uint(4096)
	req := &dto.OpenAIResponsesRequest{
		Model:             "claude-test",
		Instructions:      mustRawMessage(t, "system rules"),
		Stream:            &stream,
		MaxOutputTokens:   &maxOutputTokens,
		ParallelToolCalls: mustRawMessage(t, parallelToolCalls),
		Reasoning:         &dto.Reasoning{Effort: "medium"},
		Input: mustRawMessage(t, []map[string]any{
			{
				"role":    "user",
				"content": "question",
			},
			{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "I will call."},
				},
			},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": map[string]any{"q": "x"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  map[string]any{"ok": true},
			},
		}),
		Tools: mustRawMessage(t, []map[string]any{
			{
				"type":        "function",
				"name":        "lookup",
				"description": "Lookup data",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"q": map[string]any{"type": "string"},
					},
				},
			},
		}),
	}

	result, err := ConvertRequest(nil, info, types.RelayFormatClaude, req)

	require.NoError(t, err)
	claudeReq, ok := result.Value.(*dto.ClaudeRequest)
	require.True(t, ok)
	assert.Equal(t, ConverterOpenAIResponsesToClaudeMessages, result.Converter)
	assert.Equal(t, []RequestStep{
		{
			Converter: ConverterOpenAIResponsesToClaudeMessages,
			From:      types.RelayFormatOpenAIResponses,
			To:        types.RelayFormatClaude,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude}, info.ConversionChain)

	system, err := kitutil.Any2Type[[]dto.ClaudeMediaMessage](claudeReq.System)
	require.NoError(t, err)
	require.Len(t, system, 1)
	assert.Equal(t, "system rules", system[0].GetText())
	require.NotNil(t, claudeReq.Stream)
	assert.True(t, *claudeReq.Stream)
	assert.Equal(t, maxOutputTokens, *claudeReq.MaxTokens)
	require.NotNil(t, claudeReq.Thinking)
	assert.Equal(t, "enabled", claudeReq.Thinking.Type)
	assert.Equal(t, 2048, claudeReq.Thinking.GetBudgetTokens())

	tools, err := kitutil.Any2Type[[]*dto.Tool](claudeReq.Tools)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "lookup", tools[0].Name)

	require.Len(t, claudeReq.Messages, 3)
	assert.Equal(t, "user", claudeReq.Messages[0].Role)
	userParts, err := claudeReq.Messages[0].ParseContent()
	require.NoError(t, err)
	require.Len(t, userParts, 1)
	assert.Equal(t, "question", userParts[0].GetText())

	assert.Equal(t, "assistant", claudeReq.Messages[1].Role)
	assistantParts, err := claudeReq.Messages[1].ParseContent()
	require.NoError(t, err)
	require.Len(t, assistantParts, 2)
	assert.Equal(t, "I will call.", assistantParts[0].GetText())
	assert.Equal(t, "tool_use", assistantParts[1].Type)
	assert.Equal(t, "call_1", assistantParts[1].Id)
	assert.Equal(t, "lookup", assistantParts[1].Name)
	assert.Equal(t, map[string]any{"q": "x"}, assistantParts[1].Input)

	assert.Equal(t, "user", claudeReq.Messages[2].Role)
	toolResultParts, err := claudeReq.Messages[2].ParseContent()
	require.NoError(t, err)
	require.Len(t, toolResultParts, 1)
	assert.Equal(t, "tool_result", toolResultParts[0].Type)
	assert.Equal(t, "call_1", toolResultParts[0].ToolUseId)
	assert.Equal(t, map[string]any{"ok": true}, toolResultParts[0].Content)
}

func TestConvertRequestViaResponsesToGeminiStillUsesDirectSteps(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain:     []types.RelayFormat{types.RelayFormatOpenAIResponses},
		ChannelMetaAttached: true,
		UpstreamModelName:   "gemini-test",
	}
	req := &dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"role":    "user",
				"content": "hello",
			},
		}),
	}

	result, err := ConvertRequestVia(nil, info, req, types.RelayFormatOpenAI, types.RelayFormatGemini)

	require.NoError(t, err)
	require.IsType(t, &dto.GeminiChatRequest{}, result.Value)
	assert.Equal(t, ConverterOpenAIResponsesToOpenAIChat+","+ConverterOpenAIChatToGeminiContent, result.Converter)
	assert.Equal(t, []RequestStep{
		{
			Converter: ConverterOpenAIResponsesToOpenAIChat,
			From:      types.RelayFormatOpenAIResponses,
			To:        types.RelayFormatOpenAI,
		},
		{
			Converter: ConverterOpenAIChatToGeminiContent,
			From:      types.RelayFormatOpenAI,
			To:        types.RelayFormatGemini,
		},
	}, result.Steps)
}

func TestConvertRequestByIDDeduplicatesConversionChain(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses},
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequestByID(nil, info, ConverterOpenAIChatToOpenAIResponses, req)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	require.Len(t, result.Steps, 1)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

func TestConvertRequestByIDExecutesDirectClaudeToResponsesConverter(t *testing.T) {
	info := &convmeta.Values{
		ConversionChain: []types.RelayFormat{types.RelayFormatClaude},
	}
	req := &dto.ClaudeRequest{
		Model: "claude-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}

	result, err := ConvertRequestByID(nil, info, requestConverterClaudeToResponses, req)

	require.NoError(t, err)
	require.IsType(t, &dto.OpenAIResponsesRequest{}, result.Value)
	assert.Equal(t, requestConverterClaudeToResponses, result.Converter)
	assert.Equal(t, RequestConverterQualityFair, result.Quality)
	assert.Equal(t, []RequestStep{
		{
			Converter: requestConverterClaudeToResponses,
			From:      types.RelayFormatClaude,
			To:        types.RelayFormatOpenAIResponses,
		},
	}, result.Steps)
	assert.Equal(t, []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses}, info.ConversionChain)
}

func TestConvertRequestRejectsUnsupportedConverterAndNilRequest(t *testing.T) {
	_, err := ConvertRequestByID(nil, &convmeta.Values{}, "missing_converter", &dto.GeneralOpenAIRequest{Model: "gpt-test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")

	_, err = ConvertRequest(nil, &convmeta.Values{}, types.RelayFormatOpenAIResponses, (*dto.GeneralOpenAIRequest)(nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestConvertRequestByIDRejectsWrongSourceFormat(t *testing.T) {
	_, err := ConvertRequestByID(
		nil,
		&convmeta.Values{},
		ConverterOpenAIChatToOpenAIResponses,
		&dto.ClaudeRequest{Model: "claude-test"},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "expects openai request")
}

func TestConvertRequestRejectsUnregisteredExplicitPath(t *testing.T) {
	_, err := ConvertRequest(
		nil,
		&convmeta.Values{},
		types.RelayFormatEmbedding,
		&dto.ClaudeRequest{Model: "claude-test"},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "from claude to embedding is not registered")
}

func mustRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return raw
}

func inputContentText(t *testing.T, item map[string]any) string {
	t.Helper()
	content, ok := item["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	part, ok := content[0].(map[string]any)
	require.True(t, ok)
	text, ok := part["text"].(string)
	require.True(t, ok)
	return text
}

func TestConvertRequestRecordsResponsesCustomToolsForEachTarget(t *testing.T) {
	codexRequest := func(tools ...map[string]any) *dto.OpenAIResponsesRequest {
		return &dto.OpenAIResponsesRequest{
			Model:           "model-test",
			MaxOutputTokens: lo.ToPtr(uint(64)),
			Input:           mustMarshalRequestJSON(t, "hello"),
			Tools:           mustMarshalRequestJSON(t, tools),
		}
	}
	execTool := map[string]any{"type": "custom", "name": "exec", "description": "Run code", "format": map[string]any{"type": "text"}}
	waitTool := map[string]any{"type": "function", "name": "wait", "parameters": map[string]any{"type": "object"}}

	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(target), func(t *testing.T) {
			info := &convmeta.Values{}
			_, err := ConvertRequest(nil, info, target, codexRequest(execTool, waitTool))
			require.NoError(t, err)
			require.NotNil(t, info.ResponsesTools)
			assert.True(t, info.ResponsesTools.IsCustomTool("exec"))
			assert.False(t, info.ResponsesTools.IsCustomTool("wait"))

			_, err = ConvertRequest(nil, info, target, codexRequest(waitTool))
			require.NoError(t, err)
			assert.Nil(t, info.ResponsesTools, "a retry without custom tools must not keep the previous record")
		})
	}
}

func mustMarshalRequestJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return raw
}

func TestOpenAIResponsesRequestToClaudeMessagesNormalizesCodexTools(t *testing.T) {
	maxTokens := uint(8192)
	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), &convmeta.Values{UpstreamModelName: "claude-test"}, &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxTokens,
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":    "tool_search_call",
				"call_id": "call_tool_search_1",
				"arguments": map[string]any{
					"query": "Gmail",
					"limit": 5,
				},
			},
			{
				"type":    "tool_search_output",
				"call_id": "call_tool_search_1",
				"tools": []map[string]any{
					{
						"type": "namespace",
						"name": "mcp__codex_apps__gmail",
						"tools": []map[string]any{
							{
								"type":        "function",
								"name":        "_search_emails",
								"description": "Search Gmail",
								"parameters":  map[string]any{"type": "object"},
							},
						},
					},
				},
			},
			{
				"role":    "user",
				"content": "search mail",
			},
		}),
		Tools: mustRawMessage(t, []any{
			map[string]any{
				"type":        "function",
				"name":        "lookup",
				"description": "Lookup data",
				"parameters": map[string]any{
					"properties": map[string]any{
						"q": map[string]any{"type": "string"},
					},
				},
			},
			map[string]any{
				"type": "custom",
				"name": "apply_patch",
			},
			map[string]any{"type": "tool_search"},
			map[string]any{
				"type": "namespace",
				"name": "plugin_ns",
				"tools": []map[string]any{
					{
						"type":        "function",
						"name":        "read_doc",
						"description": "Read a doc",
						"parameters":  map[string]any{"type": "object"},
					},
				},
			},
			map[string]any{"type": "web_search"},
			map[string]any{"type": "local_shell"},
		}),
		ToolChoice: mustRawMessage(t, map[string]any{
			"type": "custom",
			"name": "apply_patch",
		}),
	})
	require.NoError(t, err)

	tools, err := kitutil.Any2Type[[]*dto.Tool](got.Tools)
	require.NoError(t, err)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		require.NotNil(t, tool)
		names = append(names, tool.Name)
		if len(tool.InputSchema) > 0 {
			assert.Equal(t, "object", mustClaudeToolSchema(t, tool)["type"])
		}
	}
	assert.Subset(t, names, []string{
		"lookup",
		"apply_patch",
		"tool_search",
		"plugin_ns__read_doc",
		"mcp__codex_apps__gmail___search_emails",
	})

	// custom freeform tool becomes a Claude tool with input schema
	assert.NotEmpty(t, tools[1].Description)
	assert.Equal(t, []any{"input"}, mustClaudeToolSchema(t, tools[1])["required"])

	choice, err := kitutil.Any2Type[dto.ClaudeToolChoice](got.ToolChoice)
	require.NoError(t, err)
	assert.Equal(t, "tool", choice.Type)
	assert.Equal(t, "apply_patch", choice.Name)

	// History starts with tool_use, so a synthetic leading user is injected.
	// tool_search_call becomes assistant tool_use named tool_search.
	require.GreaterOrEqual(t, len(got.Messages), 3)
	assert.Equal(t, "user", got.Messages[0].Role)

	assistantParts, err := kitutil.Any2Type[[]dto.ClaudeMediaMessage](got.Messages[1].Content)
	require.NoError(t, err)
	require.NotEmpty(t, assistantParts)
	assert.Equal(t, "assistant", got.Messages[1].Role)
	assert.Equal(t, "tool_use", assistantParts[0].Type)
	assert.Equal(t, "tool_search", assistantParts[0].Name)
	assert.Equal(t, "call_tool_search_1", assistantParts[0].Id)

	// tool_search_output becomes user tool_result
	assert.Equal(t, "user", got.Messages[2].Role)
}

func TestOpenAIResponsesRequestToClaudeMessagesDropsToolChoiceWithoutTools(t *testing.T) {
	maxTokens := uint(256)
	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), &convmeta.Values{UpstreamModelName: "claude-test"}, &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxTokens,
		Input:           mustRawMessage(t, "hello"),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "local_shell"},
		}),
		ToolChoice: mustRawMessage(t, "auto"),
	})
	require.NoError(t, err)
	assert.Empty(t, got.Tools)
	assert.Nil(t, got.ToolChoice)
}

func TestOpenAIResponsesRequestToClaudeMessagesFlattensNamespacedCalls(t *testing.T) {
	maxTokens := uint(256)
	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), &convmeta.Values{UpstreamModelName: "claude-test"}, &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxTokens,
		Tools: mustRawMessage(t, []map[string]any{
			{
				"type": "namespace",
				"name": "mcp__codex_apps__gmail",
				"tools": []map[string]any{
					{
						"type":        "function",
						"name":        "_search_emails",
						"description": "Search",
						"parameters":  map[string]any{"type": "object"},
					},
				},
			},
		}),
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":      "function_call",
				"call_id":   "call_ns",
				"name":      "_search_emails",
				"namespace": "mcp__codex_apps__gmail",
				"arguments": map[string]any{"query": "unread"},
			},
		}),
		ToolChoice: mustRawMessage(t, map[string]any{
			"type":      "function",
			"name":      "_search_emails",
			"namespace": "mcp__codex_apps__gmail",
		}),
	})
	require.NoError(t, err)

	tools, err := kitutil.Any2Type[[]*dto.Tool](got.Tools)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "mcp__codex_apps__gmail___search_emails", tools[0].Name)

	// Synthetic leading user precedes the assistant tool_use turn.
	require.GreaterOrEqual(t, len(got.Messages), 2)
	assert.Equal(t, "user", got.Messages[0].Role)
	assert.Equal(t, "assistant", got.Messages[1].Role)
	parts, err := kitutil.Any2Type[[]dto.ClaudeMediaMessage](got.Messages[1].Content)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "mcp__codex_apps__gmail___search_emails", parts[0].Name)

	choice, err := kitutil.Any2Type[dto.ClaudeToolChoice](got.ToolChoice)
	require.NoError(t, err)
	assert.Equal(t, "mcp__codex_apps__gmail___search_emails", choice.Name)
}

func mustClaudeToolSchema(t *testing.T, tool *dto.Tool) map[string]any {
	t.Helper()
	var schema map[string]any
	require.NoError(t, kitutil.Unmarshal(tool.InputSchema, &schema))
	return schema
}

func TestCodexToolSearchRoundTripAndRetryState(t *testing.T) {
	info := &convmeta.Values{}
	request := &dto.OpenAIResponsesRequest{Model: "gpt-test", Input: mustRawMessage(t, "find a tool"), Tools: mustRawMessage(t, []map[string]any{{"type": "tool_search"}})}
	converted, err := ConvertRequest(context.Background(), info, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	chat := converted.Value.(*dto.GeneralOpenAIRequest)
	require.Len(t, chat.Tools, 1)
	assert.Equal(t, "tool_search", chat.Tools[0].Function.Name)
	assert.JSONEq(t, `[{"type":"tool_search"}]`, string(request.Tools), "conversion must not mutate the request used by retries")
	var response dto.OpenAITextResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gpt-test","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_search","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"mail\"}"}}]},"finish_reason":"tool_calls"}]}`), &response))
	result, err := ConvertResponse(context.Background(), info, types.RelayFormatOpenAIResponses, &response)
	require.NoError(t, err)
	output := result.Value.(*dto.OpenAIResponsesResponse).Output
	require.Len(t, output, 1)
	assert.Equal(t, "tool_search_call", output[0].Type)
	assert.Equal(t, "client", output[0].Execution)
	assert.JSONEq(t, `{"query":"mail"}`, string(output[0].Arguments))
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{ID: "response_search", Model: "gpt-test"})
	require.NoError(t, err)
	var chunk dto.ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"mail\"}"}}]},"finish_reason":"tool_calls"}]}`), &chunk))
	events, err := ConvertStreamResponseChunk(context.Background(), info, state, &chunk)
	require.NoError(t, err)
	final, err := FinalizeStreamResponse(context.Background(), info, state)
	require.NoError(t, err)
	events = append(events, final...)
	found := false
	for _, event := range events {
		data := event.Value.(ChatToResponsesStreamEvent)
		if data.Type == "response.output_item.done" && data.Payload.Item != nil && data.Payload.Item.Type == "tool_search_call" {
			found = true
			assert.Equal(t, "client", data.Payload.Item.Execution)
			assert.JSONEq(t, `{"query":"mail"}`, string(data.Payload.Item.Arguments))
		}
	}
	assert.True(t, found, "stream must return the discovery call in a Responses tool-search item")
	_, err = ConvertRequest(context.Background(), info, types.RelayFormatOpenAI, &dto.OpenAIResponsesRequest{Model: "gpt-test", Input: mustRawMessage(t, "no tools")})
	require.NoError(t, err)
	assert.Nil(t, info.ResponsesToolState(), "a retry on another channel must clear old tool identities")
}

func TestCodexLegacyToolCarriersKeepSchemasAndNamespace(t *testing.T) {
	info := &convmeta.Values{}
	request := &dto.OpenAIResponsesRequest{Model: "gpt-test", Input: mustRawMessage(t, "use legacy tools"), Tools: json.RawMessage(`[{"type":"namespace","name":"plugin","children":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"z":{"type":"string"},"a":{"type":"string"}}}}}]},"apply_patch"]`)}
	result, err := ConvertRequest(context.Background(), info, types.RelayFormatOpenAI, request)
	require.NoError(t, err)
	tools := result.Value.(*dto.GeneralOpenAIRequest).Tools
	require.Len(t, tools, 2)
	assert.Equal(t, "plugin__lookup", tools[0].Function.Name)
	assert.Contains(t, string(tools[0].Function.Parameters), `"z":{"type":"string"},"a":{"type":"string"}`, "schema member order remains intact")
	assert.True(t, info.ResponsesToolState().IsCustomTool("apply_patch"))
	assert.Equal(t, "apply_patch", tools[1].Function.Name)
	assert.Contains(t, string(request.Tools), `"children"`, "normalizing a retry must not rewrite the client body")
}
