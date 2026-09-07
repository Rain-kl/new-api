package oaichat

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatRequestToClaudeMessagesNormalizesToolInputSchema(t *testing.T) {
	tests := []struct {
		name       string
		parameters any
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
			name: "missing type and properties",
			parameters: map[string]any{
				"additionalProperties": false,
			},
			wantSchema: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			name: "non-string type",
			parameters: map[string]any{
				"type":       123,
				"properties": map[string]any{},
			},
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
			assert.Equal(t, tt.wantSchema, tool.InputSchema)
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

