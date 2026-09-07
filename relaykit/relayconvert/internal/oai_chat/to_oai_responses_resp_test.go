package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatCompletionsResponseToResponsesPreservesTextToolCallsAndUsage(t *testing.T) {
	chat := &dto.OpenAITextResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 456,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message:      assistantMessageWithTool("I will call.", "call_1", "lookup", `{"q":"x"}`),
				FinishReason: "tool_calls",
			},
		},
		Usage: dto.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
	}

	resp, usage, err := ChatCompletionsResponseToResponsesResponse(chat, "resp_1")
	require.NoError(t, err)
	require.NotNil(t, usage)

	assert.Equal(t, "resp_1", resp.ID)
	assert.Equal(t, "response", resp.Object)
	assert.Equal(t, `"completed"`, string(resp.Status))
	assert.Equal(t, 3, resp.Usage.InputTokens)
	assert.Equal(t, 5, resp.Usage.OutputTokens)
	require.Len(t, resp.Output, 2)
	assert.Equal(t, responsesOutputTypeMessage, resp.Output[0].Type)
	assert.Equal(t, "I will call.", resp.Output[0].Content[0].Text)
	assert.Equal(t, responsesOutputTypeFunctionCall, resp.Output[1].Type)
	assert.Equal(t, "call_1", resp.Output[1].CallId)
	assert.Equal(t, "fc_call_1", resp.Output[1].ID)
	assert.Equal(t, "lookup", resp.Output[1].Name)
	assert.Equal(t, `"{\"q\":\"x\"}"`, string(resp.Output[1].Arguments))
}
func TestChatCompletionsResponseToResponsesRestoresCodexToolShapes(t *testing.T) {
	bridge := &convmeta.CodexToolBridge{}
	bridge.Set("apply_patch", convmeta.CodexToolSpec{Kind: convmeta.CodexToolKindCustom, Name: "apply_patch"})
	bridge.Set("tool_search", convmeta.CodexToolSpec{Kind: convmeta.CodexToolKindToolSearch, Name: "tool_search"})
	bridge.Set("mcp__codex_apps__gmail___search_emails", convmeta.CodexToolSpec{
		Kind:      convmeta.CodexToolKindNamespace,
		Name:      "_search_emails",
		Namespace: "mcp__codex_apps__gmail",
	})

	msg := dto.Message{Role: "assistant"}
	msg.SetToolCalls([]dto.ToolCallRequest{
		{ID: "call_patch", Type: "function", Function: dto.FunctionRequest{Name: "apply_patch", Arguments: `{"input":"*** Begin Patch\n*** End Patch"}`}},
		{ID: "call_search", Type: "function", Function: dto.FunctionRequest{Name: "tool_search", Arguments: `{"query":"Gmail","limit":5}`}},
		{ID: "call_ns", Type: "function", Function: dto.FunctionRequest{Name: "mcp__codex_apps__gmail___search_emails", Arguments: `{"query":"unread"}`}},
	})
	chat := &dto.OpenAITextResponse{
		Model: "gpt-test",
		Choices: []dto.OpenAITextResponseChoice{
			{Message: msg, FinishReason: "tool_calls"},
		},
	}

	resp, _, err := ChatCompletionsResponseToResponsesResponseWithBridge(chat, "resp_1", bridge)
	require.NoError(t, err)
	require.Len(t, resp.Output, 3)

	assert.Equal(t, responsesOutputTypeCustomToolCall, resp.Output[0].Type)
	assert.Equal(t, "ctc_call_patch", resp.Output[0].ID)
	assert.Equal(t, "call_patch", resp.Output[0].CallId)
	assert.Equal(t, "apply_patch", resp.Output[0].Name)
	assert.Equal(t, "*** Begin Patch\n*** End Patch", resp.Output[0].Input)

	assert.Equal(t, responsesOutputTypeToolSearchCall, resp.Output[1].Type)
	assert.Equal(t, "call_search", resp.Output[1].CallId)
	assert.Equal(t, "client", resp.Output[1].Execution)
	assert.JSONEq(t, `{"query":"Gmail","limit":5}`, string(resp.Output[1].Arguments))

	assert.Equal(t, responsesOutputTypeFunctionCall, resp.Output[2].Type)
	assert.Equal(t, "_search_emails", resp.Output[2].Name)
	assert.Equal(t, "mcp__codex_apps__gmail", resp.Output[2].Namespace)
	assert.Equal(t, `"{\"query\":\"unread\"}"`, string(resp.Output[2].Arguments))
}

func TestChatCompletionsStreamToResponsesRestoresCustomToolInputEvents(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_1", "gpt-test")
	state.ToolBridge = &convmeta.CodexToolBridge{}
	state.ToolBridge.Set("apply_patch", convmeta.CodexToolSpec{Kind: convmeta.CodexToolKindCustom, Name: "apply_patch"})
	toolIndex := 0

	var events []ChatToResponsesStreamEvent
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, ID: "call_patch", Type: "function", Function: dto.FunctionResponse{Name: "apply_patch"}},
			}}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, Function: dto.FunctionResponse{Arguments: `{"input":"patch body"}`}},
			}}},
		},
	})...)
	finishReason := "tool_calls"
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, FinishReason: &finishReason},
		},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	var sawItemAdded, sawInputDelta, sawInputDone bool
	for _, event := range events {
		switch event.Type {
		case responsesEventOutputItemAdded:
			require.NotNil(t, event.Payload.Item)
			assert.Equal(t, responsesOutputTypeCustomToolCall, event.Payload.Item.Type)
			assert.Equal(t, "ctc_call_patch", event.Payload.Item.ID)
			sawItemAdded = true
		case responsesEventCustomToolInputDelta:
			assert.Equal(t, "patch body", event.Payload.Delta)
			sawInputDelta = true
		case responsesEventCustomToolInputDone:
			assert.Equal(t, "patch body", event.Payload.Input)
			sawInputDone = true
		case responsesEventFunctionArgsDelta:
			t.Fatalf("custom tools must not emit function_call_arguments.delta")
		}
	}
	assert.True(t, sawItemAdded)
	assert.True(t, sawInputDelta)
	assert.True(t, sawInputDone)
}

func TestChatCompletionsResponseToResponsesEmitsReasoningSummaryBeforeText(t *testing.T) {
	message := dto.Message{Role: "assistant", Content: "final answer"}
	message.ReasoningContent = lo.ToPtr("thinking summary")
	resp, _, err := ChatCompletionsResponseToResponsesResponse(&dto.OpenAITextResponse{
		Id:    "chatcmpl_1",
		Model: "gpt-test",
		Choices: []dto.OpenAITextResponseChoice{
			{Message: message, FinishReason: "stop"},
		},
	}, "resp_1")
	require.NoError(t, err)

	require.Len(t, resp.Output, 2)
	assert.Equal(t, responsesOutputTypeReasoning, resp.Output[0].Type)
	require.Len(t, resp.Output[0].Summary, 1)
	assert.Equal(t, "thinking summary", resp.Output[0].Summary[0].Text)
	assert.Empty(t, resp.Output[0].Content)
	assert.Equal(t, responsesOutputTypeMessage, resp.Output[1].Type)
	assert.Equal(t, "final answer", resp.Output[1].Content[0].Text)
}


func TestChatCompletionsResponseToResponsesMapsIncompleteFinishReasons(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
		wantReason   string
	}{
		{name: "length", finishReason: "length", wantReason: responsesIncompleteReasonMaxTokens},
		{name: "content filter", finishReason: "content_filter", wantReason: responsesIncompleteReasonContentFilter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, _, err := ChatCompletionsResponseToResponsesResponse(&dto.OpenAITextResponse{
				Id:    "chatcmpl_1",
				Model: "gpt-test",
				Choices: []dto.OpenAITextResponseChoice{
					{
						Message:      dto.Message{Role: "assistant", Content: "partial"},
						FinishReason: tt.finishReason,
					},
				},
			}, "resp_1")
			require.NoError(t, err)

			assert.Equal(t, `"incomplete"`, string(resp.Status))
			require.NotNil(t, resp.IncompleteDetails)
			assert.Equal(t, tt.wantReason, resp.IncompleteDetails.Reason)
			require.Len(t, resp.Output, 1)
			assert.Equal(t, "incomplete", resp.Output[0].Status)
		})
	}
}

func TestChatCompletionsStreamToResponsesEventsAggregatesUsageAndToolArgs(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_1", "gpt-test")
	state.Created = 123
	toolIndex := 0

	var events []ChatToResponsesStreamEvent
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: 123,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: lo.ToPtr("hello")}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, ID: "call_1", Type: "function", Function: dto.FunctionResponse{Name: "lookup"}},
			}}},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{
				{Index: &toolIndex, Function: dto.FunctionResponse{Arguments: `{"q":"x"}`}},
			}}},
		},
	})...)
	finishReason := "tool_calls"
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{Index: 0, FinishReason: &finishReason},
		},
	})...)
	events = append(events, mustResponsesEventsFromChatChunk(t, state, &dto.ChatCompletionsStreamResponse{
		Usage: &dto.Usage{PromptTokens: 2, CompletionTokens: 4, TotalTokens: 6},
	})...)
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)

	require.Len(t, events, 10)
	assert.Equal(t, responsesEventCreated, events[0].Type)
	assert.Equal(t, responsesEventOutputTextDelta, events[2].Type)
	assert.Equal(t, "hello", events[2].Payload.Delta)
	assert.Equal(t, responsesEventFunctionArgsDelta, events[4].Type)
	assert.Equal(t, `{"q":"x"}`, events[4].Payload.Delta)
	assert.Equal(t, responsesEventCompleted, events[9].Type)
	require.NotNil(t, events[9].Payload.Response)
	assert.Equal(t, 6, events[9].Payload.Response.Usage.TotalTokens)
	require.Len(t, events[9].Payload.Response.Output, 2)
	assert.Equal(t, "hello", events[9].Payload.Response.Output[0].Content[0].Text)
	assert.Equal(t, `"{\"q\":\"x\"}"`, string(events[9].Payload.Response.Output[1].Arguments))
}

func mustResponsesEventsFromChatChunk(t *testing.T, state *ChatToResponsesStreamState, chunk *dto.ChatCompletionsStreamResponse) []ChatToResponsesStreamEvent {
	t.Helper()
	events, err := ChatCompletionsStreamChunkToResponsesEvents(chunk, state)
	require.NoError(t, err)
	return events
}
