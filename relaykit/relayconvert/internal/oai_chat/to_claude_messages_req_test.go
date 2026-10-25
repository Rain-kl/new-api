package oaichat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToClaudeMessagesNormalizesToolInputSchema(t *testing.T) {
	tests := []struct {
		name       string
		parameters json.RawMessage
		wantSchema map[string]any
	}{
		{
			name:       "omitted parameters",
			parameters: nil,
			wantSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			name:       "missing type and properties",
			parameters: json.RawMessage(`{"additionalProperties":false}`),
			wantSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			name:       "non-string type",
			parameters: json.RawMessage(`{"type":123,"properties":{}}`),
			wantSchema: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxTokens := uint(1024)
			got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
				Model:     "claude-test",
				MaxTokens: &maxTokens,
				Messages: []dto.Message{
					{Role: "user", Content: "Call the tool."},
				},
				Tools: []dto.ToolCallRequest{
					{
						Type: "function",
						Function: dto.FunctionRequest{
							Name:        "get_current_time",
							Description: "Get the current time",
							Parameters:  tt.parameters,
						},
					},
				},
			})

			require.NoError(t, err)
			tools, ok := got.Tools.([]any)
			require.True(t, ok)
			require.Len(t, tools, 1)
			tool, ok := tools[0].(*dto.Tool)
			require.True(t, ok)
			assert.Equal(t, "get_current_time", tool.Name)
			wantSchema, err := kitutil.Marshal(tt.wantSchema)
			require.NoError(t, err)
			assert.JSONEq(t, string(wantSchema), string(tool.InputSchema))
		})
	}
}

func TestOpenAIChatRequestToClaudeMessagesMergesAdjacentAssistantToolCalls(t *testing.T) {
	toolCall := func(id string) []dto.ToolCallRequest {
		return []dto.ToolCallRequest{{
			ID:       id,
			Type:     "function",
			Function: dto.FunctionRequest{Name: "lookup_" + id, Arguments: `{"q":"` + id + `"}`},
		}}
	}
	assistant := func(content any, toolCalls []dto.ToolCallRequest) dto.Message {
		message := dto.Message{Role: "assistant", Content: content}
		if toolCalls != nil {
			message.SetToolCalls(toolCalls)
		}
		return message
	}

	tests := []struct {
		name         string
		messages     []dto.Message
		wantTexts    []string
		wantToolUses []string
	}{
		{
			name: "both assistants have tool calls",
			messages: []dto.Message{
				{Role: "user", Content: "Look up a and b."},
				assistant("", toolCall("call_a")),
				assistant("", toolCall("call_b")),
				{Role: "tool", ToolCallId: "call_a", Content: "result a"},
				{Role: "tool", ToolCallId: "call_b", Content: "result b"},
			},
			wantTexts:    []string{"..."},
			wantToolUses: []string{"call_a", "call_b"},
		},
		{
			name: "tool calls followed by plain text",
			messages: []dto.Message{
				{Role: "user", Content: "Look up a."},
				assistant(nil, toolCall("call_a")),
				assistant("Checking a.", nil),
				{Role: "tool", ToolCallId: "call_a", Content: "result a"},
			},
			wantTexts:    []string{"Checking a."},
			wantToolUses: []string{"call_a"},
		},
		{
			name: "literal ellipsis with tool calls is preserved",
			messages: []dto.Message{
				{Role: "user", Content: "Look up a and b."},
				assistant("...", toolCall("call_a")),
				assistant("Checking b.", toolCall("call_b")),
				{Role: "tool", ToolCallId: "call_a", Content: "result a"},
				{Role: "tool", ToolCallId: "call_b", Content: "result b"},
			},
			wantTexts:    []string{"... Checking b."},
			wantToolUses: []string{"call_a", "call_b"},
		},
		{
			name: "literal ellipsis replacing a placeholder survives the next merge",
			messages: []dto.Message{
				{Role: "user", Content: "Look up a."},
				assistant(nil, toolCall("call_a")),
				assistant("...", nil),
				assistant("Checking a.", nil),
				{Role: "tool", ToolCallId: "call_a", Content: "result a"},
			},
			wantTexts:    []string{"... Checking a."},
			wantToolUses: []string{"call_a"},
		},
		{
			name: "literal ellipsis before tool calls survives repeated merges",
			messages: []dto.Message{
				{Role: "user", Content: "Look up a."},
				assistant("...", nil),
				assistant("", toolCall("call_a")),
				assistant("Checking a.", nil),
				{Role: "tool", ToolCallId: "call_a", Content: "result a"},
			},
			wantTexts:    []string{"... Checking a."},
			wantToolUses: []string{"call_a"},
		},
		{
			name: "three tool-only assistants keep one placeholder",
			messages: []dto.Message{
				{Role: "user", Content: "Look up a, b and c."},
				assistant(nil, toolCall("call_a")),
				assistant("", toolCall("call_b")),
				assistant("", toolCall("call_c")),
				{Role: "tool", ToolCallId: "call_a", Content: "result a"},
				{Role: "tool", ToolCallId: "call_b", Content: "result b"},
				{Role: "tool", ToolCallId: "call_c", Content: "result c"},
			},
			wantTexts:    []string{"..."},
			wantToolUses: []string{"call_a", "call_b", "call_c"},
		},
		{
			name: "both assistants are plain text",
			messages: []dto.Message{
				{Role: "user", Content: "Hello."},
				assistant("First.", nil),
				assistant("Second.", nil),
			},
			wantTexts: []string{"First. Second."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxTokens := uint(1024)
			got, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
				Model:     "claude-test",
				MaxTokens: &maxTokens,
				Messages:  tt.messages,
			})
			require.NoError(t, err)

			var wantToolResults []dto.ClaudeMediaMessage
			for _, message := range tt.messages {
				if message.Role == "tool" {
					wantToolResults = append(wantToolResults, dto.ClaudeMediaMessage{
						Type:      "tool_result",
						ToolUseId: message.ToolCallId,
						Content:   message.Content,
					})
				}
			}
			if len(wantToolResults) > 0 {
				require.Len(t, got.Messages, 3)
				assert.Equal(t, "user", got.Messages[2].Role)
				assert.Equal(t, wantToolResults, got.Messages[2].Content)
			} else {
				require.Len(t, got.Messages, 2)
			}
			assert.Equal(t, "assistant", got.Messages[1].Role)

			var texts, toolUses []string
			switch content := got.Messages[1].Content.(type) {
			case string:
				texts = append(texts, content)
			case []dto.ClaudeMediaMessage:
				for _, block := range content {
					switch block.Type {
					case "text":
						texts = append(texts, block.GetText())
					case "tool_use":
						toolUses = append(toolUses, block.Id)
						assert.Equal(t, "lookup_"+block.Id, block.Name)
						assert.Equal(t, map[string]any{"q": block.Id}, block.Input)
					}
				}
			default:
				require.Failf(t, "unexpected assistant content type", "%T", content)
			}
			assert.Equal(t, tt.wantTexts, texts)
			assert.Equal(t, tt.wantToolUses, toolUses)
		})
	}
}

func TestOpenAIChatRequestToClaudeMessagesReasoningClearsSamplingKnobs(t *testing.T) {
	req := dto.GeneralOpenAIRequest{
		Model:           "claude-3-7-sonnet-20250219",
		ReasoningEffort: "high",
		Temperature:     kitutil.GetPointer(0.7),
		TopP:            kitutil.GetPointer(0.9),
		MaxTokens:       kitutil.GetPointer[uint](8192),
		Messages: []dto.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	claudeReq, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, req)
	require.NoError(t, err)
	require.NotNil(t, claudeReq)

	require.NotNil(t, claudeReq.Thinking)
	assert.Equal(t, "enabled", claudeReq.Thinking.Type)
	assert.Equal(t, 6553, claudeReq.Thinking.GetBudgetTokens())

	// Temperature and TopP must be cleared when thinking is enabled
	assert.Nil(t, claudeReq.Temperature)
	assert.Nil(t, claudeReq.TopP)
}

func TestOpenAIChatRequestToClaudeMessagesReasoningBudgetTokensCapping(t *testing.T) {
	// Request with high effort (default 4096 budget) but low max_tokens (1800)
	req := dto.GeneralOpenAIRequest{
		Model:           "claude-3-7-sonnet-20250219",
		ReasoningEffort: "high",
		Temperature:     kitutil.GetPointer(0.7),
		MaxTokens:       kitutil.GetPointer[uint](1800),
		Messages: []dto.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	claudeReq, err := OpenAIChatRequestToClaudeMessages(context.Background(), nil, req)
	require.NoError(t, err)
	require.NotNil(t, claudeReq)

	if claudeReq.Thinking != nil {
		assert.Less(t, int(claudeReq.Thinking.GetBudgetTokens()), int(*claudeReq.MaxTokens))
		assert.Nil(t, claudeReq.Temperature)
	} else {
		assert.Equal(t, kitutil.GetPointer(0.7), claudeReq.Temperature)
	}
}

func TestOpenAIChatRequestToGeminiGenerateContentMergesConsecutiveUserRoles(t *testing.T) {
	req := dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{
			{Role: "user", Content: "First message"},
			{Role: "user", Content: "Second message"},
		},
	}

	geminiReq, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), req, nil)
	require.NoError(t, err)
	require.NotNil(t, geminiReq)

	// Should merge consecutive user turns into 1 content block with 2 parts
	require.Len(t, geminiReq.Contents, 1)
	assert.Equal(t, "user", geminiReq.Contents[0].Role)
	assert.Len(t, geminiReq.Contents[0].Parts, 2)
	assert.Equal(t, "First message", geminiReq.Contents[0].Parts[0].Text)
	assert.Equal(t, "Second message", geminiReq.Contents[0].Parts[1].Text)
}

