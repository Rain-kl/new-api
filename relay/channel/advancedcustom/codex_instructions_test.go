package advancedcustom

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexBaseInstructionsForModel(t *testing.T) {
	tests := []struct {
		model    string
		contains string
	}{
		{"gpt-5.3-codex", "You are Codex, based on GPT-5"},
		{"gpt-5.1-codex-max", "You are Codex, based on GPT-5"},
		{"codex-auto-review", "You are Codex, based on GPT-5"},
		{"gpt-5.5", "You are Codex, a coding agent based on GPT-5"},
		{"gpt-5.5-pro", "You are Codex, a coding agent based on GPT-5"},
		{"gpt-5.2", "You are GPT-5.2 running in the Codex CLI"},
		{"gpt-5.1", "You are GPT-5.1 running in the Codex CLI"},
		{"gpt-5.4", "You are Codex, a coding agent based on GPT-5"},
		{"unknown-model", "You are Codex, a coding agent based on GPT-5"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := CodexBaseInstructionsForModel(tt.model)
			assert.True(t, strings.Contains(got, tt.contains), "expected model %s prompt to contain %q, got %q", tt.model, tt.contains, got)
		})
	}
}

func TestIsInstructionsEmpty(t *testing.T) {
	assert.True(t, IsInstructionsEmpty(nil))
	assert.True(t, IsInstructionsEmpty(json.RawMessage(``)))
	assert.True(t, IsInstructionsEmpty(json.RawMessage(`""`)))
	assert.True(t, IsInstructionsEmpty(json.RawMessage(`"   "`)))
	assert.True(t, IsInstructionsEmpty(json.RawMessage(`null`)))

	assert.False(t, IsInstructionsEmpty(json.RawMessage(`"You are a helpful assistant."`)))
}

func TestApplyCodexInstructions(t *testing.T) {
	t.Run("injects instructions when empty", func(t *testing.T) {
		req := &dto.OpenAIResponsesRequest{
			Model: "gpt-5.3-codex",
		}
		ApplyCodexInstructions(req)

		require.NotEmpty(t, req.Instructions)
		var str string
		err := json.Unmarshal(req.Instructions, &str)
		require.NoError(t, err)
		assert.Contains(t, str, "You are Codex, based on GPT-5")
	})

	t.Run("preserves existing instructions when not empty", func(t *testing.T) {
		custom := `"Custom instructions provided by user"`
		req := &dto.OpenAIResponsesRequest{
			Model:        "gpt-5.3-codex",
			Instructions: json.RawMessage(custom),
		}
		ApplyCodexInstructions(req)

		assert.Equal(t, json.RawMessage(custom), req.Instructions)
	})

	t.Run("integrated in ConvertOpenAIResponsesRequest when CodexCompatEnabled is true", func(t *testing.T) {
		adaptor := &Adaptor{}
		info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/responses",
					UpstreamPath: "https://upstream.example/v1/responses",
					Converter:    relayconvert.ConverterNone,
				},
			},
		})
		info.ChannelSetting.CodexCompatEnabled = true

		req := dto.OpenAIResponsesRequest{
			Model: "gpt-5.2",
		}

		convertedAny, err := adaptor.ConvertOpenAIResponsesRequest(advancedCustomGinContext("/v1/responses"), info, req)
		require.NoError(t, err)

		converted, ok := convertedAny.(dto.OpenAIResponsesRequest)
		require.True(t, ok)
		require.NotEmpty(t, converted.Instructions)

		var str string
		err = json.Unmarshal(converted.Instructions, &str)
		require.NoError(t, err)
		assert.Contains(t, str, "You are GPT-5.2 running in the Codex CLI")
	})

	t.Run("not injected when CodexCompatEnabled is false", func(t *testing.T) {
		adaptor := &Adaptor{}
		info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/responses",
					UpstreamPath: "https://upstream.example/v1/responses",
					Converter:    relayconvert.ConverterNone,
				},
			},
		})
		info.ChannelSetting.CodexCompatEnabled = false

		req := dto.OpenAIResponsesRequest{
			Model: "gpt-5.2",
		}

		convertedAny, err := adaptor.ConvertOpenAIResponsesRequest(advancedCustomGinContext("/v1/responses"), info, req)
		require.NoError(t, err)

		converted, ok := convertedAny.(dto.OpenAIResponsesRequest)
		require.True(t, ok)
		assert.Empty(t, converted.Instructions)
	})
}
