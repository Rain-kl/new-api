package sub2api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/codexcompat"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLAlphaSearch(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeSub2API,
			ChannelBaseUrl: "https://sub2api.example",
		},
		RequestURLPath: "/v1/alpha/search",
		RelayMode:      relayconstant.RelayModeAlphaSearch,
	}

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://sub2api.example/v1/alpha/search", url)
}

func TestAdaptorInheritsNewAPIResponsesCompactSupport(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeSub2API,
			ChannelBaseUrl: "https://sub2api.example",
		},
		RequestURLPath: "/v1/responses/compact",
		RelayMode:      relayconstant.RelayModeResponsesCompact,
	}

	url, err := adaptor.GetRequestURL(info)

	require.NoError(t, err)
	assert.Equal(t, "https://sub2api.example/v1/responses/compact", url)
	assert.Equal(t, "sub2api", adaptor.GetChannelName())
	assert.Empty(t, adaptor.GetModelList())
}

func TestSetupRequestHeader_CompatOff_NoForcedOriginator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &Adaptor{}
	info := baseRelayInfo(false, "synthesize")
	c := testGinContext(map[string]string{
		"User-Agent": "curl/8.0",
	})
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))

	assert.Equal(t, "Bearer test-key", header.Get("Authorization"))
	assert.Empty(t, header.Get("originator"))
	assert.Empty(t, header.Get("session_id"))
	assert.Empty(t, header.Get("x-codex-window-id"))
	// Parent copies Content-Type from client; client did not send one.
	assert.Empty(t, header.Get("User-Agent"))
}

func TestSetupRequestHeader_CompatOn_SynthesizeIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &Adaptor{}
	info := baseRelayInfo(true, "synthesize")
	c := testGinContext(map[string]string{
		"User-Agent": "curl/8.0",
		"originator": "curl",
	})
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))

	assert.Equal(t, "Bearer test-key", header.Get("Authorization"))
	assert.Equal(t, "application/json", header.Get("Content-Type"))
	assert.True(t, strings.Contains(header.Get("User-Agent"), "0.146.0"), "UA=%q", header.Get("User-Agent"))
	assert.True(t, strings.HasPrefix(header.Get("User-Agent"), "codex_cli_rs/0.146.0"))
	assert.Equal(t, "codex_cli_rs", header.Get("originator"))
	assert.NotEmpty(t, header.Get("session_id"))
	assert.NotEmpty(t, header.Get("thread_id"))
	assert.NotEmpty(t, header.Get("x-codex-window-id"))
}

func TestSetupRequestHeader_CompatOn_GateCapablePreservesSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &Adaptor{}
	info := baseRelayInfo(true, "auto")
	c := testGinContext(map[string]string{
		"User-Agent": "codex_cli_rs/0.146.0 (linux; x86_64)",
		"originator": "codex_cli_rs",
		"session_id": "client-session-abc",
		"thread_id":  "client-thread-xyz",
	})
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))

	assert.Equal(t, "Bearer test-key", header.Get("Authorization"))
	assert.Equal(t, "codex_cli_rs/0.146.0 (linux; x86_64)", header.Get("User-Agent"))
	assert.Equal(t, "codex_cli_rs", header.Get("originator"))
	assert.Equal(t, "client-session-abc", header.Get("session_id"))
	assert.Equal(t, "client-thread-xyz", header.Get("thread_id"))
	// No inbound x-codex-* → inject sticky window fingerprint.
	assert.NotEmpty(t, header.Get("x-codex-window-id"))
}

func TestSetupRequestHeader_CompactMode_AcceptJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &Adaptor{}
	info := baseRelayInfo(true, "synthesize")
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	c := testGinContext(map[string]string{
		"Accept": "text/event-stream",
	})
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))

	assert.Equal(t, "application/json", header.Get("Accept"))
	assert.Equal(t, "application/json", header.Get("Content-Type"))
}

func TestConvertOpenAIResponsesRequest_InjectsPromptCacheKeyWhenEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &Adaptor{}
	info := baseRelayInfo(true, "auto")
	c := testGinContext(nil)

	out, err := adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: json.RawMessage(`"hi"`),
	})
	require.NoError(t, err)
	req, ok := out.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	var pck string
	require.NoError(t, common.Unmarshal(req.PromptCacheKey, &pck))
	assert.NotEmpty(t, pck)

	// Same sticky seed as header resolution without inbound conversation keys.
	expected := codexcompat.ResolveStickyIDs(codexcompat.StickyInput{
		ChannelID: 10,
		UserID:    1,
		TokenID:   2,
	})
	assert.Equal(t, expected.PromptCacheKey, pck)
}

func TestConvertOpenAIResponsesRequest_CompatOff_Unchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &Adaptor{}
	info := baseRelayInfo(false, "auto")
	c := testGinContext(nil)

	in := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: json.RawMessage(`"hi"`),
	}
	out, err := adaptor.ConvertOpenAIResponsesRequest(c, info, in)
	require.NoError(t, err)
	req, ok := out.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.Empty(t, req.PromptCacheKey)
}

func TestConvertOpenAIResponsesRequest_StripsEmptyInstructionsPreservesKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &Adaptor{}
	info := baseRelayInfo(true, "auto")
	c := testGinContext(nil)

	out, err := adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
		Model:          "gpt-5",
		Instructions:   json.RawMessage(`""`),
		PromptCacheKey: json.RawMessage(`"client-cache-key"`),
	})
	require.NoError(t, err)
	req, ok := out.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.Nil(t, req.Instructions)

	var pck string
	require.NoError(t, common.Unmarshal(req.PromptCacheKey, &pck))
	assert.Equal(t, "client-cache-key", pck)
}

// Body prompt_cache_key must seed sticky thread_id in SetupRequestHeader so
// synthesize headers stay aligned with the outbound body (design §8.5).
func TestSetupRequestHeader_BodyPromptCacheKeyAlignsThreadID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &Adaptor{}
	bodyReq := &dto.OpenAIResponsesRequest{
		Model:          "gpt-5",
		Input:          json.RawMessage(`"hi"`),
		PromptCacheKey: json.RawMessage(`"client-cache-key"`),
	}
	info := baseRelayInfo(true, "synthesize")
	info.Request = bodyReq
	// Non-gate-capable inbound, no thread header.
	c := testGinContext(map[string]string{
		"User-Agent": "curl/8.0",
	})
	header := http.Header{}

	require.NoError(t, adaptor.SetupRequestHeader(c, &header, info))
	assert.Equal(t, "client-cache-key", header.Get("thread_id"))

	out, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *bodyReq)
	require.NoError(t, err)
	converted, ok := out.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	var pck string
	require.NoError(t, common.Unmarshal(converted.PromptCacheKey, &pck))
	assert.Equal(t, "client-cache-key", pck)
}

func baseRelayInfo(compat bool, mode string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:  1,
		TokenId: 2,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeSub2API,
			ChannelId:   10,
			ApiKey:      "test-key",
			ChannelSetting: dto.ChannelSettings{
				CodexCompatEnabled: compat,
				CodexClientVersion: "0.146.0",
				CodexIdentityMode:  mode,
			},
		},
	}
}

func testGinContext(headers map[string]string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.Request = req
	return c
}
