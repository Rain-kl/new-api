package oaichat

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.Equal(t, 4096, claudeReq.Thinking.GetBudgetTokens())

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

func TestOpenAIChatRequestToClaudeMessagesThinkingAdapterDefaultMaxTokensRetainsThinking(t *testing.T) {
	meta := &convmeta.Values{Options: &convmeta.Options{
		Claude: convmeta.ClaudeOptions{
			ThinkingAdapterEnabled:                true,
			ThinkingAdapterBudgetTokensPercentage: 0.8,
		},
	}}
	req := dto.GeneralOpenAIRequest{
		Model: "claude-3-7-sonnet-thinking",
		Messages: []dto.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	claudeReq, err := OpenAIChatRequestToClaudeMessages(context.Background(), meta, req)
	require.NoError(t, err)
	require.NotNil(t, claudeReq)
	require.NotNil(t, claudeReq.Thinking)
	assert.Equal(t, "enabled", claudeReq.Thinking.Type)
	assert.GreaterOrEqual(t, claudeReq.Thinking.GetBudgetTokens(), 1024)
}

