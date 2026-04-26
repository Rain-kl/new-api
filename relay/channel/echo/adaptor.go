package echo

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct{}

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
	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return nil, nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	requestId := c.GetString(common.RequestIdKey)
	modelName := info.UpstreamModelName
	if modelName == "" {
		modelName = "echo"
	}

	echoedText := extractLastUserMessage(c)
	useTime := time.Now().Unix()

	if info.IsStream {
		return echoStreamResponse(c, requestId, modelName, echoedText, useTime)
	}
	return echoNonStreamResponse(c, requestId, modelName, echoedText, useTime)
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
	var request dto.GeneralOpenAIRequest
	if err := common.Unmarshal(bodyBytes, &request); err != nil {
		return ""
	}
	for i := len(request.Messages) - 1; i >= 0; i-- {
		if request.Messages[i].Role == "user" {
			return request.Messages[i].StringContent()
		}
	}
	return ""
}

func echoNonStreamResponse(c *gin.Context, requestId string, modelName string, text string, useTime int64) (*dto.Usage, *types.NewAPIError) {
	response := dto.OpenAITextResponse{
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
	}

	jsonResponse, err := common.Marshal(response)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	_, _ = c.Writer.Write(jsonResponse)

	return &dto.Usage{
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
	}, nil
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
