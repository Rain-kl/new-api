package oairesponses

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesRequestToChatCompletionsRequestInstructionsAndScalarInput(t *testing.T) {
	stream := true
	temperature := 0.0
	topP := 0.9
	maxOutputTokens := uint(128)
	parallelToolCalls := true

	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model:                "gpt-test",
		Instructions:         mustRawMessage(t, "system rules"),
		Input:                mustRawMessage(t, "hello"),
		Stream:               &stream,
		StreamOptions:        &dto.StreamOptions{IncludeUsage: true},
		MaxOutputTokens:      &maxOutputTokens,
		Temperature:          &temperature,
		TopP:                 &topP,
		User:                 mustRawMessage(t, "user-1"),
		Store:                mustRawMessage(t, false),
		Metadata:             mustRawMessage(t, map[string]any{"trace": "abc"}),
		ParallelToolCalls:    mustRawMessage(t, parallelToolCalls),
		PromptCacheKey:       mustRawMessage(t, "cache-key"),
		PromptCacheRetention: mustRawMessage(t, "24h"),
		Reasoning:            &dto.Reasoning{Effort: "medium"},
	})
	require.NoError(t, err)

	assert.Equal(t, "gpt-test", got.Model)
	require.Len(t, got.Messages, 2)
	assert.Equal(t, dto.Message{Role: "system", Content: "system rules"}, got.Messages[0])
	assert.Equal(t, dto.Message{Role: "user", Content: "hello"}, got.Messages[1])
	assert.Same(t, &stream, got.Stream)
	require.NotNil(t, got.StreamOptions)
	assert.True(t, got.StreamOptions.IncludeUsage)
	assert.Equal(t, maxOutputTokens, lo.FromPtr(got.MaxCompletionTokens))
	assert.Equal(t, 0.0, lo.FromPtr(got.Temperature))
	assert.Equal(t, 0.9, lo.FromPtr(got.TopP))
	// No chat-compatible tools present: drop parallel_tool_calls (cc-switch guard
	// for strict OpenAI-compatible upstreams that reject the field without tools).
	assert.Nil(t, got.ParallelTooCalls)
	assert.Equal(t, "cache-key", got.PromptCacheKey)
	assert.Equal(t, "medium", got.ReasoningEffort)
	assert.Equal(t, `"user-1"`, string(got.User))
	assert.Equal(t, `false`, string(got.Store))
	assert.Equal(t, "abc", gjson.GetBytes(got.Metadata, "trace").String())
}

func TestResponsesRequestToChatCompletionsRequestPreservesQwenThinkingBudget(t *testing.T) {
	tests := []struct {
		name   string
		budget json.RawMessage
		want   int64
	}{
		{name: "positive budget", budget: json.RawMessage(`128`), want: 128},
		{name: "zero budget", budget: json.RawMessage(`0`), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
				Model:          "qwen-plus",
				Input:          mustRawMessage(t, "hello"),
				EnableThinking: json.RawMessage(`true`),
				ThinkingBudget: tt.budget,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.budget, got.ThinkingBudget)

			encoded, err := kitutil.Marshal(got)
			require.NoError(t, err)

			assert.True(t, gjson.GetBytes(encoded, "enable_thinking").Bool())
			value := gjson.GetBytes(encoded, "thinking_budget")
			assert.True(t, value.Exists())
			assert.Equal(t, tt.want, value.Int())
		})
	}
}

func TestResponsesRequestToChatCompletionsRequestMultimodalInput(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "look"},
					{"type": "input_image", "image_url": "https://example.test/a.png", "detail": "low"},
					{"type": "input_file", "file_id": "file_1", "filename": "a.txt"},
					{"type": "input_audio", "input_audio": map[string]any{"data": "abc", "format": "wav"}},
					{"type": "input_video", "video_url": map[string]any{"url": "https://example.test/v.mp4"}},
				},
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 1)
	assert.Equal(t, "user", got.Messages[0].Role)
	parts := got.Messages[0].ParseContent()
	require.Len(t, parts, 5)
	assert.Equal(t, dto.ContentTypeText, parts[0].Type)
	assert.Equal(t, "look", parts[0].Text)
	assert.Equal(t, dto.ContentTypeImageURL, parts[1].Type)
	assert.Equal(t, "https://example.test/a.png", parts[1].GetImageMedia().Url)
	assert.Equal(t, dto.ContentTypeFile, parts[2].Type)
	assert.Equal(t, "file_1", parts[2].GetFile().FileId)
	assert.Equal(t, dto.ContentTypeInputAudio, parts[3].Type)
	assert.Equal(t, "wav", parts[3].GetInputAudio().Format)
	assert.Equal(t, dto.ContentTypeVideoUrl, parts[4].Type)
	assert.Equal(t, "https://example.test/v.mp4", parts[4].GetVideoUrl().Url)
}

func TestResponsesRequestToChatCompletionsRequestAssistantTextAndFunctionCallCoexist(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
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
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 2)
	assert.Equal(t, "assistant", got.Messages[0].Role)
	assert.Equal(t, "I will call.", got.Messages[0].StringContent())
	toolCalls := got.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_1", toolCalls[0].ID)
	assert.Equal(t, "function", toolCalls[0].Type)
	assert.Equal(t, "lookup", toolCalls[0].Function.Name)
	assert.JSONEq(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
	assert.Equal(t, "tool", got.Messages[1].Role)
	assert.Equal(t, "call_1", got.Messages[1].ToolCallId)
	assert.JSONEq(t, `{"ok":true}`, got.Messages[1].StringContent())
}

func TestResponsesRequestToChatCompletionsRequestOnlyFunctionCallCreatesAssistant(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "lookup",
				"arguments": `{"q":"x"}`,
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 1)
	assert.Equal(t, "assistant", got.Messages[0].Role)
	assert.Nil(t, got.Messages[0].Content)
	toolCalls := got.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
}

func TestResponsesRequestToChatCompletionsRequestToolsToolChoiceAndTextFormat(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, "hello"),
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
		ToolChoice: mustRawMessage(t, map[string]any{
			"type": "function",
			"name": "lookup",
		}),
		Text: mustRawMessage(t, map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "answer",
				"schema": map[string]any{"type": "object"},
				"strict": true,
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Tools, 1)
	assert.Equal(t, "function", got.Tools[0].Type)
	assert.Equal(t, "lookup", got.Tools[0].Function.Name)
	assert.Equal(t, "Lookup data", got.Tools[0].Function.Description)
	assert.Equal(t, "object", got.Tools[0].Function.Parameters.(map[string]any)["type"])
	assert.Equal(t, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "lookup",
		},
	}, got.ToolChoice)
	require.NotNil(t, got.ResponseFormat)
	assert.Equal(t, "json_schema", got.ResponseFormat.Type)
	assert.Equal(t, "answer", gjson.GetBytes(got.ResponseFormat.JsonSchema, "name").String())
	assert.True(t, gjson.GetBytes(got.ResponseFormat.JsonSchema, "strict").Bool())
}

func TestResponsesRequestToChatCompletionsRequestCustomToolCallMapsToFunctionInput(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":    "custom_tool_call",
				"call_id": "call_custom",
				"name":    "apply_patch",
				"input":   "patch body",
			},
		}),
	})
	require.NoError(t, err)

	require.Len(t, got.Messages, 1)
	toolCalls := got.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "function", toolCalls[0].Type)
	assert.Equal(t, "call_custom", toolCalls[0].ID)
	assert.Equal(t, "apply_patch", toolCalls[0].Function.Name)
	assert.JSONEq(t, `{"input":"patch body"}`, toolCalls[0].Function.Arguments)
}

func TestResponsesRequestToChatCompletionsRequestNormalizesCodexToolsLikeCCS(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, []map[string]any{
			{
				"type":    "tool_search_call",
				"call_id": "call_tool_search_1",
				"arguments": map[string]any{
					"query": "Gmail search emails",
					"limit": 5,
				},
			},
			{
				"type":    "tool_search_output",
				"call_id": "call_tool_search_1",
				"tools": []map[string]any{
					{
						"type":        "namespace",
						"name":        "mcp__codex_apps__gmail",
						"description": "Find and reference emails from your inbox.",
						"tools": []map[string]any{
							{
								"type":        "function",
								"name":        "_search_emails",
								"description": "Search Gmail for emails matching a query.",
								"parameters": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"query": map[string]any{"type": "string"},
									},
									"required": []string{"query"},
								},
							},
						},
					},
				},
			},
			{
				"role":    "user",
				"content": "Search unread inbox mail.",
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
				"type":        "custom",
				"name":        "apply_patch",
				"description": "Apply a patch to files.",
				"format": map[string]any{
					"type":   "grammar",
					"syntax": "lark",
				},
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
						"parameters":   map[string]any{"type": "object"},
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
		ParallelToolCalls: mustRawMessage(t, true),
	})
	require.NoError(t, err)

	toolNames := make([]string, 0, len(got.Tools))
	for _, tool := range got.Tools {
		assert.Equal(t, "function", tool.Type)
		toolNames = append(toolNames, tool.Function.Name)
	}
	assert.Equal(t, []string{
		"lookup",
		"apply_patch",
		"tool_search",
		"plugin_ns__read_doc",
		"mcp__codex_apps__gmail___search_emails",
	}, toolNames)

	// parameters.type must be object even when source omitted it
	assert.Equal(t, "object", got.Tools[0].Function.Parameters.(map[string]any)["type"])

	// custom tool becomes a chat function with freeform input schema
	assert.Equal(t, "apply_patch", got.Tools[1].Function.Name)
	assert.Contains(t, got.Tools[1].Function.Description, "Original tool definition:")
	params := got.Tools[1].Function.Parameters.(map[string]any)
	assert.Equal(t, "object", params["type"])
	assert.Equal(t, []any{"input"}, params["required"])

	assert.Equal(t, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "apply_patch",
		},
	}, got.ToolChoice)

	// historical tool_search_call becomes a normal function tool call
	require.GreaterOrEqual(t, len(got.Messages), 2)
	searchCalls := got.Messages[0].ParseToolCalls()
	require.Len(t, searchCalls, 1)
	assert.Equal(t, "function", searchCalls[0].Type)
	assert.Equal(t, "tool_search", searchCalls[0].Function.Name)
	assert.Equal(t, "call_tool_search_1", searchCalls[0].ID)
	assert.JSONEq(t, `{"limit":5,"query":"Gmail search emails"}`, searchCalls[0].Function.Arguments)
	assert.Equal(t, "tool", got.Messages[1].Role)
	assert.Equal(t, "call_tool_search_1", got.Messages[1].ToolCallId)
}

func TestResponsesRequestToChatCompletionsRequestDropsToolChoiceWhenNoChatTools(t *testing.T) {
	parallel := true
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, "hello"),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "web_search"},
			{"type": "local_shell"},
		}),
		ToolChoice:        mustRawMessage(t, "auto"),
		ParallelToolCalls: mustRawMessage(t, parallel),
	})
	require.NoError(t, err)
	assert.Empty(t, got.Tools)
	assert.Nil(t, got.ToolChoice)
	assert.Nil(t, got.ParallelTooCalls)
}

func TestResponsesRequestToChatCompletionsRequestAttachesCodexToolBridge(t *testing.T) {
	meta := &convmeta.Values{}
	_, err := ResponsesRequestToChatCompletionsRequestWithMeta(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustRawMessage(t, "hello"),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "custom", "name": "apply_patch"},
			{"type": "tool_search"},
			{
				"type": "namespace",
				"name": "plugin_ns",
				"tools": []map[string]any{
					{"type": "function", "name": "read_doc", "parameters": map[string]any{"type": "object"}},
				},
			},
		}),
	}, meta)
	require.NoError(t, err)

	bridge := meta.EnsureCodexToolBridge()
	custom, ok := bridge.Lookup("apply_patch")
	require.True(t, ok)
	assert.Equal(t, convmeta.CodexToolKindCustom, custom.Kind)
	search, ok := bridge.Lookup("tool_search")
	require.True(t, ok)
	assert.Equal(t, convmeta.CodexToolKindToolSearch, search.Kind)
	ns, ok := bridge.Lookup("plugin_ns__read_doc")
	require.True(t, ok)
	assert.Equal(t, convmeta.CodexToolKindNamespace, ns.Kind)
	assert.Equal(t, "read_doc", ns.Name)
	assert.Equal(t, "plugin_ns", ns.Namespace)
}

func TestResponsesRequestToChatCompletionsRequestFlattensNamespacedFunctionCalls(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Tools: mustRawMessage(t, []map[string]any{
			{
				"type": "namespace",
				"name": "mcp__codex_apps__gmail",
				"tools": []map[string]any{
					{
						"type":        "function",
						"name":        "_search_emails",
						"description": "Search",
						"parameters":   map[string]any{"type": "object"},
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

	require.Len(t, got.Tools, 1)
	assert.Equal(t, "mcp__codex_apps__gmail___search_emails", got.Tools[0].Function.Name)

	toolCalls := got.Messages[0].ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "mcp__codex_apps__gmail___search_emails", toolCalls[0].Function.Name)
	assert.Equal(t, map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "mcp__codex_apps__gmail___search_emails",
		},
	}, got.ToolChoice)
}

func TestResponsesRequestToChatCompletionsRequestRejectsStatefulFields(t *testing.T) {
	tests := []struct {
		name string
		req  *dto.OpenAIResponsesRequest
		want string
	}{
		{
			name: "conversation",
			req:  &dto.OpenAIResponsesRequest{Model: "gpt-test", Conversation: mustRawMessage(t, "conv_1")},
			want: "conversation",
		},
		{
			name: "previous response",
			req:  &dto.OpenAIResponsesRequest{Model: "gpt-test", PreviousResponseID: "resp_1"},
			want: "previous_response_id",
		},
		{
			name: "prompt",
			req:  &dto.OpenAIResponsesRequest{Model: "gpt-test", Prompt: mustRawMessage(t, map[string]any{"id": "pmpt_1"})},
			want: "prompt",
		},
		{
			name: "context management",
			req:  &dto.OpenAIResponsesRequest{Model: "gpt-test", ContextManagement: mustRawMessage(t, map[string]any{"type": "auto"})},
			want: "context_management",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResponsesRequestToChatCompletionsRequest(tt.req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Contains(t, err.Error(), "stateful fields")
		})
	}
}

func mustRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return raw
}
