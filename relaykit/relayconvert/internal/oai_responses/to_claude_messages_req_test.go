package oairesponses

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestToClaudeMessagesNormalizesCodexTools(t *testing.T) {
	maxTokens := uint(8192)
	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, &dto.OpenAIResponsesRequest{
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
								"parameters":   map[string]any{"type": "object"},
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
	})
	require.NoError(t, err)

	tools, err := kitutil.Any2Type[[]*dto.Tool](got.Tools)
	require.NoError(t, err)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		require.NotNil(t, tool)
		names = append(names, tool.Name)
		assert.Equal(t, "object", tool.InputSchema["type"])
	}
	assert.Equal(t, []string{
		"lookup",
		"apply_patch",
		"tool_search",
		"plugin_ns__read_doc",
		"mcp__codex_apps__gmail___search_emails",
	}, names)

	// custom freeform tool becomes a Claude tool with input schema
	assert.Contains(t, tools[1].Description, "Original tool definition:")
	assert.Equal(t, []any{"input"}, tools[1].InputSchema["required"])

	choice, ok := got.ToolChoice.(*dto.ClaudeToolChoice)
	require.True(t, ok)
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
	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, &dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxTokens,
		Input:           mustRawMessage(t, "hello"),
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "web_search"},
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
	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, &dto.OpenAIResponsesRequest{
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

	choice, ok := got.ToolChoice.(*dto.ClaudeToolChoice)
	require.True(t, ok)
	assert.Equal(t, "mcp__codex_apps__gmail___search_emails", choice.Name)
}
