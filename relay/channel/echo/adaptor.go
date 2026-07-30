package echo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct{}

const contextKeyEchoText = "echo_text"

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return info.ChannelBaseUrl, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	setEchoText(c, extractLastUserMessageFromOpenAIRequest(request))
	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	setEchoText(c, extractTextFromResponsesRequest(&request))
	return &request, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	openaiRequest, err := service.GeminiToOpenAIRequest(request, info)
	if err != nil {
		return nil, err
	}
	setEchoText(c, extractLastUserMessageFromOpenAIRequest(openaiRequest))
	return openaiRequest, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	openaiRequest, err := service.ClaudeToOpenAIRequest(*request, info)
	if err != nil {
		return nil, err
	}
	setEchoText(c, extractLastUserMessageFromOpenAIRequest(openaiRequest))
	return openaiRequest, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if requestBody != nil {
		body, err := io.ReadAll(requestBody)
		if err != nil {
			return nil, err
		}
		if len(body) > 0 && getEchoText(c) == "" {
			setEchoText(c, extractEchoTextFromBody(body, info))
		}
	}

	if info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact {
		respBody, contentType, err := buildResponsesHTTPBody(c, info)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{contentType},
			},
			Body: io.NopCloser(bytes.NewReader(respBody)),
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}, nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	requestId := c.GetString(common.RequestIdKey)
	modelName := info.UpstreamModelName
	if modelName == "" {
		modelName = "echo"
	}

	echoedText := extractEchoText(c)
	useTime := time.Now().Unix()

	if info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact {
		responseBody, contentType, buildErr := buildResponsesHTTPBody(c, info)
		if buildErr != nil {
			return nil, types.NewOpenAIError(buildErr, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}
		c.Writer.Header().Set("Content-Type", contentType)
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.Write(responseBody)
		return zeroUsage(), nil
	}

	if info.IsStream {
		return echoStreamResponse(c, requestId, modelName, echoedText, useTime)
	}
	return echoNonStreamResponse(c, info, requestId, modelName, echoedText, useTime)
}

func setEchoText(c *gin.Context, text string) {
	if c == nil || text == "" {
		return
	}
	c.Set(contextKeyEchoText, text)
}

func getEchoText(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString(contextKeyEchoText)
}

func extractEchoText(c *gin.Context) string {
	if text := getEchoText(c); text != "" {
		return text
	}
	return extractLastUserMessage(c)
}

func extractLastUserMessage(c *gin.Context) string {
	storage, storageErr := common.GetBodyStorage(c)
	if storageErr != nil {
		return ""
	}
	bodyBytes, err := storage.Bytes()
	if err != nil {
		return ""
	}
	return extractEchoTextFromBody(bodyBytes, nil)
}

func extractEchoTextFromBody(bodyBytes []byte, info *relaycommon.RelayInfo) string {
	if info != nil && (info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact) {
		var request dto.OpenAIResponsesRequest
		if err := common.Unmarshal(bodyBytes, &request); err == nil {
			return extractTextFromResponsesRequest(&request)
		}
	}
	var request dto.GeneralOpenAIRequest
	if err := common.Unmarshal(bodyBytes, &request); err != nil {
		return ""
	}
	return extractLastUserMessageFromOpenAIRequest(&request)
}

func extractLastUserMessageFromOpenAIRequest(request *dto.GeneralOpenAIRequest) string {
	if request == nil {
		return ""
	}
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == "user" {
			return request.Messages[i].StringContent()
		}
	}
	inputs := request.ParseInput()
	if len(inputs) > 0 {
		return strings.Join(inputs, "\n")
	}
	return ""
}

func extractTextFromResponsesRequest(request *dto.OpenAIResponsesRequest) string {
	if request == nil {
		return ""
	}
	inputs := request.ParseInput()
	texts := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if input.Text != "" {
			texts = append(texts, input.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func zeroUsage() *dto.Usage {
	return &dto.Usage{
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		InputTokens:      0,
		OutputTokens:     0,
	}
}

func echoTextResponse(requestId string, modelName string, text string, useTime int64) dto.OpenAITextResponse {
	return dto.OpenAITextResponse{
		Id:      fmt.Sprintf("echocmpl-%s", requestId),
		Object:  "chat.completion",
		Created: useTime,
		Model:   modelName,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index: 0,
				Message: dto.Message{
					Role:    "assistant",
					Content: text,
				},
				FinishReason: "stop",
			},
		},
		Usage: *zeroUsage(),
	}
}

func echoResponsesResponse(requestId string, modelName string, text string, useTime int64) dto.OpenAIResponsesResponse {
	status := json.RawMessage(`"completed"`)
	nullValue := json.RawMessage(`null`)
	usage := zeroUsage()
	return dto.OpenAIResponsesResponse{
		ID:        fmt.Sprintf("resp-%s", requestId),
		Object:    "response",
		CreatedAt: int(useTime),
		Status:    status,
		Model:     modelName,
		Output: []dto.ResponsesOutput{
			{
				Type:   "message",
				ID:     fmt.Sprintf("msg-%s", requestId),
				Status: "completed",
				Role:   "assistant",
				Content: []dto.ResponsesOutputContent{
					{
						Type:        "output_text",
						Text:        text,
						Annotations: []interface{}{},
					},
				},
			},
		},
		PreviousResponseID: nullValue,
		Instructions:       nullValue,
		ToolChoice:         json.RawMessage(`"auto"`),
		Truncation:         json.RawMessage(`"disabled"`),
		User:               nullValue,
		Metadata:           nullValue,
		Usage:              usage,
	}
}

func buildResponsesHTTPBody(c *gin.Context, info *relaycommon.RelayInfo) ([]byte, string, error) {
	requestId := c.GetString(common.RequestIdKey)
	modelName := info.UpstreamModelName
	if modelName == "" {
		modelName = "echo"
	}
	useTime := time.Now().Unix()
	response := echoResponsesResponse(requestId, modelName, extractEchoText(c), useTime)
	if info.IsStream {
		created := dto.ResponsesStreamResponse{
			Type:     "response.created",
			Response: &response,
		}
		delta := dto.ResponsesStreamResponse{
			Type:  "response.output_text.delta",
			Delta: extractEchoText(c),
		}
		completed := dto.ResponsesStreamResponse{
			Type:     "response.completed",
			Response: &response,
		}
		events := []dto.ResponsesStreamResponse{created, delta, completed}
		var builder strings.Builder
		for _, event := range events {
			eventJSON, err := common.Marshal(event)
			if err != nil {
				return nil, "", err
			}
			builder.WriteString("data: ")
			builder.Write(eventJSON)
			builder.WriteString("\n\n")
		}
		builder.WriteString("data: [DONE]\n\n")
		return []byte(builder.String()), "text/event-stream", nil
	}
	body, err := common.Marshal(response)
	if err != nil {
		return nil, "", err
	}
	return body, "application/json", nil
}

func echoNonStreamResponse(c *gin.Context, info *relaycommon.RelayInfo, requestId string, modelName string, text string, useTime int64) (*dto.Usage, *types.NewAPIError) {
	response := echoTextResponse(requestId, modelName, text, useTime)
	var responseBody []byte
	var err error
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		claudeResponse := service.ResponseOpenAI2Claude(&response, nil)
		responseBody, err = common.Marshal(claudeResponse)
	case types.RelayFormatGemini:
		geminiResponse := service.ResponseOpenAI2Gemini(&response, nil)
		responseBody, err = common.Marshal(geminiResponse)
	default:
		responseBody, err = common.Marshal(response)
	}
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(responseBody)

	return zeroUsage(), nil
}

func echoStreamResponse(c *gin.Context, requestId string, modelName string, text string, useTime int64) (*dto.Usage, *types.NewAPIError) {
	helper.SetEventStreamHeaders(c)

	finishReason := "stop"
	role := "assistant"
	chunk := dto.ChatCompletionsStreamResponse{
		Id:      fmt.Sprintf("echocmpl-%s", requestId),
		Object:  "chat.completion.chunk",
		Created: useTime,
		Model:   modelName,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    role,
					Content: &text,
				},
				FinishReason: &finishReason,
			},
		},
	}

	jsonChunk, err := common.Marshal(chunk)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	c.Stream(func(w io.Writer) bool {
		if _, writeErr := fmt.Fprintf(w, "data: %s\n\n", string(jsonChunk)); writeErr != nil {
			return false
		}
		if _, writeErr := fmt.Fprintf(w, "data: [DONE]\n\n"); writeErr != nil {
			return false
		}
		return false
	})

	return &dto.Usage{
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
	}, nil
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
