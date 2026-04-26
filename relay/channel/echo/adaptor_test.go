package echo

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func setupEchoTestContext(method string, path string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(common.RequestIdKey, "test-request")
	return c, recorder
}

func TestEchoChatCompletionResponse(t *testing.T) {
	body := `{"model":"echo","messages":[{"role":"user","content":"Hello, world"}]}`
	c, recorder := setupEchoTestContext(http.MethodPost, "/v1/chat/completions", body)

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		RelayMode:   relayconstant.RelayModeChatCompletions,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "echo",
		},
	}
	request := &dto.GeneralOpenAIRequest{
		Model: "echo",
		Messages: []dto.Message{
			{Role: "user", Content: "Hello, world"},
		},
	}

	if _, err := adaptor.ConvertOpenAIRequest(c, info, request); err != nil {
		t.Fatalf("ConvertOpenAIRequest returned error: %v", err)
	}

	usage, apiErr := adaptor.DoResponse(c, nil, info)
	if apiErr != nil {
		t.Fatalf("DoResponse returned error: %v", apiErr)
	}
	if usage.(*dto.Usage).TotalTokens != 0 {
		t.Fatalf("expected zero usage, got %+v", usage)
	}

	var response dto.OpenAITextResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if got := response.Choices[0].Message.StringContent(); got != "Hello, world" {
		t.Fatalf("expected echoed content, got %q", got)
	}
}

func TestEchoResponsesRequest(t *testing.T) {
	body := `{"model":"echo","input":"Hello from responses"}`
	c, _ := setupEchoTestContext(http.MethodPost, "/v1/responses", body)

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "echo",
		},
	}
	request := dto.OpenAIResponsesRequest{
		Model: "echo",
		Input: []byte(`"Hello from responses"`),
	}

	converted, err := adaptor.ConvertOpenAIResponsesRequest(c, info, request)
	if err != nil {
		t.Fatalf("ConvertOpenAIResponsesRequest returned error: %v", err)
	}
	jsonData, err := common.Marshal(converted)
	if err != nil {
		t.Fatalf("failed to marshal converted request: %v", err)
	}

	respAny, err := adaptor.DoRequest(c, info, bytes.NewReader(jsonData))
	if err != nil {
		t.Fatalf("DoRequest returned error: %v", err)
	}
	resp := respAny.(*http.Response)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var response dto.OpenAIResponsesResponse
	if err := common.Unmarshal(respBody, &response); err != nil {
		t.Fatalf("failed to unmarshal responses body: %v", err)
	}
	if got := response.Output[0].Content[0].Text; got != "Hello from responses" {
		t.Fatalf("expected echoed responses content, got %q", got)
	}
}
