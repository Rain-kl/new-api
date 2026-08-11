package advancedcustom

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptorUsesExactRouteAndQueryAuth(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "https://upstream.example/v1/chat/completions?existing=1",
				Converter:    relayconvert.ConverterClaudeMessagesToOpenAIChat,
				Auth: &dto.AdvancedCustomRouteAuth{
					Type:  dto.AdvancedCustomAuthTypeQuery,
					Name:  "api_key",
					Value: "{api_key}",
				},
			},
		},
	})
	info.RequestURLPath = "/v1/messages?client=1"

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)

	parsedURL, err := url.Parse(requestURL)
	require.NoError(t, err)
	assert.Equal(t, "https", parsedURL.Scheme)
	assert.Equal(t, "upstream.example", parsedURL.Host)
	assert.Equal(t, "/v1/chat/completions", parsedURL.Path)
	assert.Equal(t, "1", parsedURL.Query().Get("existing"))
	assert.Equal(t, "sk-test", parsedURL.Query().Get("api_key"))
}

func TestAdaptorJoinsUpstreamPathWithChannelBaseURL(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "/proxy/v1/chat/completions?existing=1",
				Converter:    relayconvert.ConverterNone,
				Auth: &dto.AdvancedCustomRouteAuth{
					Type:  dto.AdvancedCustomAuthTypeQuery,
					Name:  "api_key",
					Value: "{api_key}",
				},
			},
		},
	})
	info.ChannelBaseUrl = "https://gateway.example/base"

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)

	parsedURL, err := url.Parse(requestURL)
	require.NoError(t, err)
	assert.Equal(t, "https", parsedURL.Scheme)
	assert.Equal(t, "gateway.example", parsedURL.Host)
	assert.Equal(t, "/base/proxy/v1/chat/completions", parsedURL.Path)
	assert.Equal(t, "1", parsedURL.Query().Get("existing"))
	assert.Equal(t, "sk-test", parsedURL.Query().Get("api_key"))
}

func TestAdaptorReturnsErrorWhenUpstreamPathNeedsMissingBaseURL(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "/v1/chat/completions",
				Converter:    relayconvert.ConverterNone,
			},
		},
	})
	info.ChannelBaseUrl = ""

	_, err := adaptor.GetRequestURL(info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base URL is required")
}

func TestAdaptorSetupRequestHeaderUsesDefaultBearerAuth(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "https://upstream.example/v1/chat/completions",
				Converter:    relayconvert.ConverterNone,
			},
		},
	})
	c := advancedCustomGinContext("/v1/chat/completions")
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
	assert.Equal(t, "Bearer sk-test", header.Get("Authorization"))
}

func TestAdaptorSetupRequestHeaderClaudeFormatDefaultAuthUsesXApiKey(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "https://upstream.example/v1/messages",
				Converter:    relayconvert.ConverterNone,
			},
		},
	})
	info.RelayFormat = types.RelayFormatClaude
	c := advancedCustomGinContext("/v1/messages")
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
	// Anthropic-compatible upstreams read x-api-key, not Authorization: Bearer.
	assert.Empty(t, header.Get("Authorization"))
	assert.Equal(t, "sk-test", header.Get("x-api-key"))
	assert.Equal(t, "2023-06-01", header.Get("anthropic-version"))
}

func TestAdaptorSetupRequestHeaderUsesConfiguredHeaderAuth(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "https://upstream.example/v1/chat/completions",
				Converter:    relayconvert.ConverterNone,
				Auth: &dto.AdvancedCustomRouteAuth{
					Type:  dto.AdvancedCustomAuthTypeHeader,
					Name:  "x-api-key",
					Value: "{api_key}",
				},
			},
		},
	})
	c := advancedCustomGinContext("/v1/chat/completions")
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
	assert.Empty(t, header.Get("Authorization"))
	assert.Equal(t, "sk-test", header.Get("x-api-key"))
}

func TestAdaptorSetupRequestHeaderAddsClaudeDefaultHeaders(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "https://api.anthropic.com/v1/messages",
				Converter:    relayconvert.ConverterNone,
				Auth: &dto.AdvancedCustomRouteAuth{
					Type:  dto.AdvancedCustomAuthTypeHeader,
					Name:  "x-api-key",
					Value: "{api_key}",
				},
			},
		},
	})
	info.RelayFormat = types.RelayFormatClaude
	c := advancedCustomGinContext("/v1/messages")
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
	assert.Equal(t, "sk-test", header.Get("x-api-key"))
	assert.Equal(t, "2023-06-01", header.Get("anthropic-version"))
}

func TestAdaptorReturnsErrorWhenNoRouteMatchesPath(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "https://upstream.example/v1/chat/completions",
				Converter:    relayconvert.ConverterClaudeMessagesToOpenAIChat,
			},
		},
	})
	info.RequestURLPath = "/v1/chat/completions"

	_, err := adaptor.GetRequestURL(info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support request path")
}

func TestAdaptorResolvePlaygroundPathMatchesOpenAIChatRoute(t *testing.T) {
	// Playground requests (/pg/chat/completions) are the same OpenAI chat API as
	// /v1/chat/completions; incomingRequestPath must normalize so the route matches
	// (mirroring Distribute's selection-phase normalization).
	adaptor := &Adaptor{}
	c := advancedCustomGinContext("/pg/chat/completions")
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "https://upstream.example/v1/chat/completions",
				Converter:    relayconvert.ConverterNone,
			},
		},
	})
	require.NoError(t, adaptor.resolve(c, info), "playground path must match the /v1/chat/completions route")
	assert.Equal(t, "/v1/chat/completions", adaptor.route.IncomingPath)

	// A different route must still not match the playground path (fresh adaptor:
	// resolve caches its result on the instance).
	badAdaptor := &Adaptor{}
	bad := advancedCustomGinContext("/pg/chat/completions")
	badInfo := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "https://upstream.example/v1/responses",
				Converter:    relayconvert.ConverterNone,
			},
		},
	})
	require.Error(t, badAdaptor.resolve(bad, badInfo), "a different route must still not match the playground path")
}

func TestAdaptorReplacesModelPlaceholderInRouteURL(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent",
				Converter:    relayconvert.ConverterOpenAIChatToGeminiContent,
				Auth: &dto.AdvancedCustomRouteAuth{
					Type:  dto.AdvancedCustomAuthTypeQuery,
					Name:  "key",
					Value: "{api_key}",
				},
			},
		},
	})
	info.UpstreamModelName = "gemini-2.5-flash"

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)

	parsedURL, err := url.Parse(requestURL)
	require.NoError(t, err)
	assert.Equal(t, "/v1beta/models/gemini-2.5-flash:generateContent", parsedURL.Path)
	assert.Equal(t, "sk-test", parsedURL.Query().Get("key"))
	assert.Empty(t, parsedURL.Query().Get("alt"))
}

func TestAdaptorSwitchesGeminiGenerateContentURLForStream(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent?existing=1",
				Converter:    relayconvert.ConverterOpenAIChatToGeminiContent,
				Auth: &dto.AdvancedCustomRouteAuth{
					Type:  dto.AdvancedCustomAuthTypeQuery,
					Name:  "key",
					Value: "{api_key}",
				},
			},
		},
	})
	info.UpstreamModelName = "gemini-2.5-pro"
	info.IsStream = true

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)

	parsedURL, err := url.Parse(requestURL)
	require.NoError(t, err)
	assert.Equal(t, "/v1beta/models/gemini-2.5-pro:streamGenerateContent", parsedURL.Path)
	assert.Equal(t, "sse", parsedURL.Query().Get("alt"))
	assert.Equal(t, "1", parsedURL.Query().Get("existing"))
	assert.Equal(t, "sk-test", parsedURL.Query().Get("key"))
}

func TestAdaptorMatchesGeminiIncomingPathTemplate(t *testing.T) {
	tests := []struct {
		name            string
		requestURLPath  string
		wantRequestPath string
	}{
		{
			name:            "generate content",
			requestURLPath:  "/v1beta/models/gemini-2.5-flash:generateContent",
			wantRequestPath: "/v1/chat/completions",
		},
		{
			name:            "stream generate content",
			requestURLPath:  "/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse",
			wantRequestPath: "/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptor := &Adaptor{}
			info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
				Routes: []dto.AdvancedCustomRoute{
					{
						IncomingPath: "/v1beta/models/{model}:generateContent",
						UpstreamPath: "https://upstream.example/v1/chat/completions",
						Converter:    relayconvert.ConverterGeminiContentToOpenAIChat,
					},
				},
			})
			info.RequestURLPath = tt.requestURLPath

			requestURL, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)

			parsedURL, err := url.Parse(requestURL)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRequestPath, parsedURL.Path)
		})
	}
}

func TestAdaptorBuildModelListRequestUsesConfiguredRouteAuth(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/models",
				UpstreamPath: "/provider/models",
				Converter:    relayconvert.ConverterNone,
				Auth: &dto.AdvancedCustomRouteAuth{
					Type:  dto.AdvancedCustomAuthTypeHeader,
					Name:  "x-api-key",
					Value: "token {api_key}",
				},
			},
		},
	})
	info.RequestURLPath = "/v1/models"

	requestURL, header, err := adaptor.BuildModelListRequest(info)
	require.NoError(t, err)

	parsedURL, err := url.Parse(requestURL)
	require.NoError(t, err)
	assert.Equal(t, "fallback.example", parsedURL.Host)
	assert.Equal(t, "/provider/models", parsedURL.Path)
	assert.Equal(t, "token sk-test", header.Get("x-api-key"))
	assert.Empty(t, header.Get("Authorization"))
}

func TestAdaptorBuildModelListRequestUsesConfiguredQueryAuth(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/models",
				UpstreamPath: "https://upstream.example/v1/models?existing=1",
				Converter:    relayconvert.ConverterNone,
				Auth: &dto.AdvancedCustomRouteAuth{
					Type:  dto.AdvancedCustomAuthTypeQuery,
					Name:  "key",
					Value: "{api_key}",
				},
			},
		},
	})
	info.RequestURLPath = "/v1/models"

	requestURL, header, err := adaptor.BuildModelListRequest(info)
	require.NoError(t, err)

	parsedURL, err := url.Parse(requestURL)
	require.NoError(t, err)
	assert.Equal(t, "upstream.example", parsedURL.Host)
	assert.Equal(t, "/v1/models", parsedURL.Path)
	assert.Equal(t, "1", parsedURL.Query().Get("existing"))
	assert.Equal(t, "sk-test", parsedURL.Query().Get("key"))
	assert.Empty(t, header.Get("Authorization"))
}

func TestAdaptorBuildModelListRequestDefaultAndNoAuth(t *testing.T) {
	tests := []struct {
		name              string
		auth              *dto.AdvancedCustomRouteAuth
		wantAuthorization string
	}{
		{
			name:              "default bearer",
			wantAuthorization: "Bearer sk-test",
		},
		{
			name: "no authentication",
			auth: &dto.AdvancedCustomRouteAuth{
				Type: dto.AdvancedCustomAuthTypeNone,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
				Routes: []dto.AdvancedCustomRoute{
					{
						IncomingPath: dto.AdvancedCustomModelListPath,
						UpstreamPath: "/provider/models",
						Auth:         tt.auth,
					},
				},
			})
			info.RequestURLPath = "/unrelated/path"

			requestURL, header, err := (&Adaptor{}).BuildModelListRequest(info)
			require.NoError(t, err)
			assert.Equal(t, "https://fallback.example/provider/models", requestURL)
			assert.Equal(t, tt.wantAuthorization, header.Get("Authorization"))
		})
	}
}

func TestAdaptorBuildModelListRequestDoesNotReuseRelayRoute(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "/chat",
			},
			{
				IncomingPath: dto.AdvancedCustomModelListPath,
				UpstreamPath: "/provider/models",
			},
		},
	})

	chatURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://fallback.example/chat", chatURL)

	modelURL, header, err := adaptor.BuildModelListRequest(info)
	require.NoError(t, err)
	assert.Equal(t, "https://fallback.example/provider/models", modelURL)
	assert.Equal(t, "Bearer sk-test", header.Get("Authorization"))
}

func TestAdaptorBuildModelListRequestRequiresConfiguredRoute(t *testing.T) {
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "/v1/chat/completions",
			},
		},
	})

	_, _, err := (&Adaptor{}).BuildModelListRequest(info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not configure a /v1/models route")
}

func TestAdaptorConvertsResponsesRequestToOpenAIChatUpstream(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    relayconvert.ConverterOpenAIResponsesToOpenAIChat,
			},
		},
	})
	info.RelayMode = relayconstant.RelayModeResponses
	info.RequestURLPath = "/v1/responses"
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	converted, err := adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
		Model:        "gpt-test",
		Instructions: mustAdvancedCustomRawMessage(t, "system rules"),
		Input:        mustAdvancedCustomRawMessage(t, "hello"),
	})
	require.NoError(t, err)

	chatReq, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, "gpt-test", chatReq.Model)
	require.Len(t, chatReq.Messages, 2)
	assert.Equal(t, "system", chatReq.Messages[0].Role)
	assert.Equal(t, "system rules", chatReq.Messages[0].StringContent())
	assert.Equal(t, "user", chatReq.Messages[1].Role)
	assert.Equal(t, "hello", chatReq.Messages[1].StringContent())

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	parsedURL, err := url.Parse(requestURL)
	require.NoError(t, err)
	assert.Equal(t, "/v1/chat/completions", parsedURL.Path)
}

func TestAdaptorSelectsDuplicateResponsesRoutesByModel(t *testing.T) {
	config := &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/chat/completions",
				Converter:    relayconvert.ConverterOpenAIResponsesToOpenAIChat,
				Models:       []string{"gpt-test"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    relayconvert.ConverterOpenAIResponsesToGemini,
				Models:       []string{"gemini-test"},
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/messages",
				Converter:    relayconvert.ConverterOpenAIResponsesToClaudeMessages,
				Models:       []string{"claude-test"},
			},
		},
	}

	chatAdaptor := &Adaptor{}
	chatInfo := advancedCustomRelayInfo(config)
	chatInfo.RelayFormat = types.RelayFormatOpenAIResponses
	chatInfo.RelayMode = relayconstant.RelayModeResponses
	chatInfo.RequestURLPath = "/v1/responses"
	chatInfo.OriginModelName = "gpt-test"
	chatInfo.UpstreamModelName = "gpt-test"
	chatConverted, err := chatAdaptor.ConvertOpenAIResponsesRequest(advancedCustomGinContext("/v1/responses"), chatInfo, dto.OpenAIResponsesRequest{
		Model: "gpt-test",
		Input: mustAdvancedCustomRawMessage(t, "hello"),
	})
	require.NoError(t, err)
	_, ok := chatConverted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)

	geminiAdaptor := &Adaptor{}
	geminiInfo := advancedCustomRelayInfo(config)
	geminiInfo.RelayFormat = types.RelayFormatOpenAIResponses
	geminiInfo.RelayMode = relayconstant.RelayModeResponses
	geminiInfo.RequestURLPath = "/v1/responses"
	geminiInfo.OriginModelName = "gemini-test"
	geminiInfo.UpstreamModelName = "gemini-test"
	geminiInfo.IsStream = true
	geminiConverted, err := geminiAdaptor.ConvertOpenAIResponsesRequest(advancedCustomGinContext("/v1/responses"), geminiInfo, dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustAdvancedCustomRawMessage(t, "hello"),
	})
	require.NoError(t, err)
	_, ok = geminiConverted.(*dto.GeminiChatRequest)
	require.True(t, ok)

	requestURL, err := geminiAdaptor.GetRequestURL(geminiInfo)
	require.NoError(t, err)
	parsedURL, err := url.Parse(requestURL)
	require.NoError(t, err)
	assert.Equal(t, "/v1beta/models/gemini-test:streamGenerateContent", parsedURL.Path)
	assert.Equal(t, "sse", parsedURL.Query().Get("alt"))

	claudeAdaptor := &Adaptor{}
	claudeInfo := advancedCustomRelayInfo(config)
	claudeInfo.RelayFormat = types.RelayFormatOpenAIResponses
	claudeInfo.RelayMode = relayconstant.RelayModeResponses
	claudeInfo.RequestURLPath = "/v1/responses"
	claudeInfo.OriginModelName = "claude-test"
	claudeInfo.UpstreamModelName = "claude-test"
	maxTokens := uint(256)
	claudeConverted, err := claudeAdaptor.ConvertOpenAIResponsesRequest(advancedCustomGinContext("/v1/responses"), claudeInfo, dto.OpenAIResponsesRequest{
		Model:           "claude-test",
		MaxOutputTokens: &maxTokens,
		Input:           mustAdvancedCustomRawMessage(t, "hello"),
		Tools: mustAdvancedCustomRawMessage(t, []map[string]any{
			{"type": "custom", "name": "apply_patch"},
			{"type": "web_search"},
		}),
	})
	require.NoError(t, err)
	claudeReq, ok := claudeConverted.(*dto.ClaudeRequest)
	require.True(t, ok)
	require.NotEmpty(t, claudeReq.Tools)
	claudeURL, err := claudeAdaptor.GetRequestURL(claudeInfo)
	require.NoError(t, err)
	assert.Contains(t, claudeURL, "/v1/messages")
}

func TestAdaptorResponsesToGeminiUsesResponsesBridge(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    relayconvert.ConverterOpenAIResponsesToGemini,
				Models:       []string{"gemini-test"},
			},
		},
	})
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	info.RequestURLPath = "/v1/responses"
	info.OriginModelName = "gemini-test"
	info.UpstreamModelName = "gemini-test"
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{Text: "hello"},
					},
				},
			},
		},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     2,
			CandidatesTokenCount: 3,
			TotalTokenCount:      5,
		},
	}
	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, newAPIError := adaptor.DoResponse(c, &http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}, info)
	require.Nil(t, newAPIError)
	require.NotNil(t, usage)

	got := recorder.Body.String()
	assert.Contains(t, got, `"object":"response"`)
	assert.Contains(t, got, `"type":"output_text"`)
	assert.Contains(t, got, `"text":"hello"`)
	assert.NotContains(t, got, `"candidates"`)
}

func TestAdaptorResponsesToGeminiAddsThoughtSignatureForFunctionCallHistory(t *testing.T) {
	geminiSettings := model_setting.GetGeminiSettings()
	originalThoughtSignatureEnabled := geminiSettings.FunctionCallThoughtSignatureEnabled
	geminiSettings.FunctionCallThoughtSignatureEnabled = true
	t.Cleanup(func() {
		geminiSettings.FunctionCallThoughtSignatureEnabled = originalThoughtSignatureEnabled
	})

	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    relayconvert.ConverterOpenAIResponsesToGemini,
				Models:       []string{"gemini-test"},
			},
		},
	})
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	info.RequestURLPath = "/v1/responses"
	info.OriginModelName = "gemini-test"
	info.UpstreamModelName = "gemini-test"

	converted, err := adaptor.ConvertOpenAIResponsesRequest(advancedCustomGinContext("/v1/responses"), info, dto.OpenAIResponsesRequest{
		Model: "gemini-test",
		Input: mustAdvancedCustomRawMessage(t, []map[string]any{
			{
				"role":    "user",
				"content": "hi",
			},
			{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "glob",
				"arguments": map[string]any{"query": "*"},
			},
			{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  []map[string]any{{"path": "report.md"}},
			},
		}),
		Tools: mustAdvancedCustomRawMessage(t, []map[string]any{
			{"type": "function", "name": "glob", "parameters": map[string]any{"type": "object"}},
		}),
	})
	require.NoError(t, err)

	geminiReq, ok := converted.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, geminiReq.Contents, 3)
	require.Len(t, geminiReq.Contents[1].Parts, 1)
	require.NotNil(t, geminiReq.Contents[1].Parts[0].FunctionCall)
	assert.NotEmpty(t, geminiReq.Contents[1].Parts[0].ThoughtSignature)
	require.Len(t, geminiReq.Contents[2].Parts, 1)
	require.NotNil(t, geminiReq.Contents[2].Parts[0].FunctionResponse)
	assert.Empty(t, geminiReq.Contents[2].Parts[0].ThoughtSignature)
}

func TestAdaptorConvertsOpenAIChatRequestToResponsesUpstream(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "/v1/responses",
				Converter:    relayconvert.ConverterOpenAIChatToOpenAIResponses,
			},
		},
	})
	c := advancedCustomGinContext("/v1/chat/completions")

	converted, err := adaptor.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{
		Model: "gpt-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	})
	require.NoError(t, err)

	responsesReq, ok := converted.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.Equal(t, "gpt-test", responsesReq.Model)
	assert.NotEmpty(t, responsesReq.Input)
}

func TestAdaptorConvertsOpenAIChatRequestToClaudeUpstream(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "/v1/messages",
				Converter:    relayconvert.ConverterOpenAIChatToClaudeMessages,
			},
		},
	})
	c := advancedCustomGinContext("/v1/chat/completions")

	converted, err := adaptor.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{
		Model: "claude-test",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	})
	require.NoError(t, err)

	claudeReq, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)
	assert.Equal(t, "claude-test", claudeReq.Model)
	require.Len(t, claudeReq.Messages, 1)
	assert.Equal(t, "user", claudeReq.Messages[0].Role)
}

func TestAdaptorConvertsOpenAIChatRequestToGeminiUpstream(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "/v1beta/models/{model}:generateContent",
				Converter:    relayconvert.ConverterOpenAIChatToGeminiContent,
			},
		},
	})
	info.UpstreamModelName = "gemini-2.5-flash"
	c := advancedCustomGinContext("/v1/chat/completions")

	converted, err := adaptor.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{
			{Role: "user", Content: "hello"},
		},
	})
	require.NoError(t, err)

	geminiReq, ok := converted.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, geminiReq.Contents, 1)
	assert.Equal(t, "user", geminiReq.Contents[0].Role)
}

func TestAdaptorConvertsClaudeRequestToOpenAIChatUpstream(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/messages",
				UpstreamPath: "/v1/chat/completions",
				Converter:    relayconvert.ConverterClaudeMessagesToOpenAIChat,
			},
		},
	})
	info.RelayFormat = types.RelayFormatClaude
	info.RequestURLPath = "/v1/messages"
	c := advancedCustomGinContext("/v1/messages")

	converted, err := adaptor.ConvertClaudeRequest(c, info, &dto.ClaudeRequest{
		Model: "gpt-test",
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	})
	require.NoError(t, err)

	chatReq, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, "gpt-test", chatReq.Model)
	require.Len(t, chatReq.Messages, 1)
	assert.Equal(t, "user", chatReq.Messages[0].Role)
}

func TestAdaptorConvertsGeminiRequestToOpenAIChatUpstream(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1beta/models/{model}:generateContent",
				UpstreamPath: "/v1/chat/completions",
				Converter:    relayconvert.ConverterGeminiContentToOpenAIChat,
			},
		},
	})
	info.RelayFormat = types.RelayFormatGemini
	info.RequestURLPath = "/v1beta/models/gemini-2.5-flash:generateContent"
	info.UpstreamModelName = "gpt-test"
	c := advancedCustomGinContext("/v1beta/models/gemini-2.5-flash:generateContent")

	converted, err := adaptor.ConvertGeminiRequest(c, info, &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role: "user",
				Parts: []dto.GeminiPart{
					{Text: "hello"},
				},
			},
		},
	})
	require.NoError(t, err)

	chatReq, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, "gpt-test", chatReq.Model)
	require.Len(t, chatReq.Messages, 1)
	assert.Equal(t, "user", chatReq.Messages[0].Role)
}

func TestAdaptorSetupRequestHeaderCodexCompat(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "https://upstream.example/v1/chat/completions",
				Converter:    relayconvert.ConverterNone,
			},
		},
	})
	info.ChannelSetting.CodexCompatEnabled = true
	info.ChannelSetting.CodexClientVersion = "0.146.0"
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(advancedCustomGinContext("/v1/chat/completions"), &header, info))
	assert.True(t, strings.HasPrefix(header.Get("User-Agent"), "codex_cli_rs/0.146.0"))
	assert.Equal(t, "codex_cli_rs", header.Get("originator"))
}

func TestAdaptorSetupRequestHeaderClaudeCompat(t *testing.T) {
	t.Run("claude-targeted route applies claude-cli fingerprint", func(t *testing.T) {
		adaptor := &Adaptor{}
		info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/responses",
					UpstreamPath: "https://api.anthropic.com/v1/messages",
					Converter:    relayconvert.ConverterOpenAIResponsesToClaudeMessages,
				},
			},
		})
		info.RelayFormat = types.RelayFormatOpenAIResponses
		info.RelayMode = relayconstant.RelayModeResponses
		info.RequestURLPath = "/v1/responses"
		info.ChannelSetting.ClaudeCompatEnabled = true
		header := http.Header{}

		require.NoError(t, adaptor.SetupRequestHeader(advancedCustomGinContext("/v1/responses"), &header, info))
		assert.Equal(t, claudeCodeUserAgent, header.Get("User-Agent"))
		assert.Equal(t, "cli", header.Get("x-app"))
		assert.Contains(t, header.Get("anthropic-beta"), claudeCodeBeta)
	})

	t.Run("non-claude route does not apply claude-cli fingerprint", func(t *testing.T) {
		adaptor := &Adaptor{}
		info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/chat/completions",
					UpstreamPath: "https://upstream.example/v1/chat/completions",
					Converter:    relayconvert.ConverterNone,
				},
			},
		})
		info.ChannelSetting.ClaudeCompatEnabled = true
		header := http.Header{}

		require.NoError(t, adaptor.SetupRequestHeader(advancedCustomGinContext("/v1/chat/completions"), &header, info))
		assert.NotEqual(t, claudeCodeUserAgent, header.Get("User-Agent"))
		assert.Empty(t, header.Get("x-app"))
		assert.Empty(t, header.Get("anthropic-beta"))
	})
}

func TestAdaptorSetupRequestHeaderNoCompat(t *testing.T) {
	adaptor := &Adaptor{}
	info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/chat/completions",
				UpstreamPath: "https://upstream.example/v1/chat/completions",
				Converter:    relayconvert.ConverterNone,
			},
		},
	})
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(advancedCustomGinContext("/v1/chat/completions"), &header, info))
	assert.Empty(t, header.Get("User-Agent"))
	assert.Empty(t, header.Get("originator"))
	assert.Empty(t, header.Get("x-app"))
	assert.Empty(t, header.Get("anthropic-beta"))
}

func TestAdaptorConvertPrependsClaudeCodeIdentity(t *testing.T) {
	convertToClaude := func(t *testing.T, adaptor *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, converter string) (*dto.ClaudeRequest, error) {
		t.Helper()
		var converted any
		var err error
		switch converter {
		case relayconvert.ConverterOpenAIChatToClaudeMessages:
			converted, err = adaptor.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{
				Model: "claude-test",
				Messages: []dto.Message{
					{Role: "system", Content: "System rules."},
					{Role: "user", Content: "hello"},
				},
			})
		case relayconvert.ConverterOpenAIResponsesToClaudeMessages:
			converted, err = adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
				Model:        "claude-test",
				Instructions: mustAdvancedCustomRawMessage(t, "System rules."),
				Input:        mustAdvancedCustomRawMessage(t, "hello"),
			})
		case relayconvert.ConverterNone:
			converted, err = adaptor.ConvertClaudeRequest(c, info, &dto.ClaudeRequest{
				Model:    "claude-test",
				System:   "System rules.",
				Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
			})
		default:
			return nil, fmt.Errorf("unexpected converter %q", converter)
		}
		if err != nil {
			return nil, err
		}
		claudeReq, ok := converted.(*dto.ClaudeRequest)
		if !ok {
			return nil, fmt.Errorf("expected *dto.ClaudeRequest, got %T", converted)
		}
		return claudeReq, nil
	}

	tests := []struct {
		name         string
		converter    string
		incomingPath string
		configure    func(info *relaycommon.RelayInfo)
	}{
		{
			name:         "openai chat completions to claude messages",
			converter:    relayconvert.ConverterOpenAIChatToClaudeMessages,
			incomingPath: "/v1/chat/completions",
		},
		{
			name:         "openai responses to claude messages",
			converter:    relayconvert.ConverterOpenAIResponsesToClaudeMessages,
			incomingPath: "/v1/responses",
			configure: func(info *relaycommon.RelayInfo) {
				info.RelayFormat = types.RelayFormatOpenAIResponses
				info.RelayMode = relayconstant.RelayModeResponses
				info.RequestURLPath = "/v1/responses"
			},
		},
		{
			name:         "native claude messages",
			converter:    relayconvert.ConverterNone,
			incomingPath: "/v1/messages",
			configure: func(info *relaycommon.RelayInfo) {
				info.RelayFormat = types.RelayFormatClaude
				info.RequestURLPath = "/v1/messages"
			},
		},
	}

	t.Run("enabled prepends identity as first system block", func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				adaptor := &Adaptor{}
				info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
					Routes: []dto.AdvancedCustomRoute{
						{
							IncomingPath: tt.incomingPath,
							UpstreamPath: "/v1/messages",
							Converter:    tt.converter,
						},
					},
				})
				if tt.configure != nil {
					tt.configure(info)
				}
				info.ChannelSetting.ClaudeCompatEnabled = true

				claudeReq, err := convertToClaude(t, adaptor, advancedCustomGinContext(tt.incomingPath), info, tt.converter)
				require.NoError(t, err)
				blocks, ok := claudeReq.System.([]dto.ClaudeMediaMessage)
				require.True(t, ok)
				require.NotEmpty(t, blocks)
				require.NotNil(t, blocks[0].Text)
				assert.Equal(t, claudeCodeSystemIdentity, *blocks[0].Text)
			})
		}
	})

	t.Run("disabled leaves system untouched", func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				adaptor := &Adaptor{}
				info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
					Routes: []dto.AdvancedCustomRoute{
						{
							IncomingPath: tt.incomingPath,
							UpstreamPath: "/v1/messages",
							Converter:    tt.converter,
						},
					},
				})
				if tt.configure != nil {
					tt.configure(info)
				}

				claudeReq, err := convertToClaude(t, adaptor, advancedCustomGinContext(tt.incomingPath), info, tt.converter)
				require.NoError(t, err)

				switch system := claudeReq.System.(type) {
				case []dto.ClaudeMediaMessage:
					require.NotEmpty(t, system)
					require.NotNil(t, system[0].Text)
					assert.NotEqual(t, claudeCodeSystemIdentity, *system[0].Text)
				case string:
					assert.NotEqual(t, claudeCodeSystemIdentity, system)
				default:
					t.Fatalf("unexpected system type %T", claudeReq.System)
				}
			})
		}
	})
}

func advancedCustomRelayInfo(config *dto.AdvancedCustomConfig) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RequestURLPath:  "/v1/chat/completions",
		OriginModelName: "gpt-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "sk-test",
			ChannelBaseUrl:    "https://fallback.example",
			ChannelType:       constant.ChannelTypeAdvancedCustom,
			UpstreamModelName: "gpt-test",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AdvancedCustom: config,
			},
		},
	}
}

func advancedCustomGinContext(path string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

func mustAdvancedCustomRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}

func TestAdaptorResponsesToChatAppliesRoleCompatibility(t *testing.T) {
	responsesInput := mustAdvancedCustomRawMessage(t, []map[string]any{
		{"role": "developer", "content": "be helpful"},
	})

	convert := func(t *testing.T, compat bool) *dto.GeneralOpenAIRequest {
		t.Helper()
		adaptor := &Adaptor{}
		info := advancedCustomRelayInfo(&dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/responses",
					UpstreamPath: "/v1/chat/completions",
					Converter:    relayconvert.ConverterOpenAIResponsesToOpenAIChat,
				},
			},
		})
		info.RelayFormat = types.RelayFormatOpenAIResponses
		info.RelayMode = relayconstant.RelayModeResponses
		info.RequestURLPath = "/v1/responses"
		info.ChannelSetting.MessagesRoleCompatibilityEnabled = compat

		out, err := adaptor.ConvertOpenAIResponsesRequest(
			advancedCustomGinContext("/v1/responses"),
			info,
			dto.OpenAIResponsesRequest{Model: "gpt-test", Input: responsesInput},
		)
		require.NoError(t, err)
		chatReq, ok := out.(*dto.GeneralOpenAIRequest)
		require.True(t, ok)
		return chatReq
	}

	t.Run("enabled remaps developer to fallback", func(t *testing.T) {
		chatReq := convert(t, true)
		require.NotEmpty(t, chatReq.Messages)
		assert.Equal(t, "system", chatReq.Messages[0].Role)
	})

	t.Run("disabled preserves developer", func(t *testing.T) {
		chatReq := convert(t, false)
		require.NotEmpty(t, chatReq.Messages)
		assert.Equal(t, "developer", chatReq.Messages[0].Role)
	})
}
