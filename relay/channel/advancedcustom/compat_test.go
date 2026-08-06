package advancedcustom

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func codexCompatRelayInfo(enabled bool, version string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat:     "openai-responses",
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "gpt-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "sk-test",
			ChannelBaseUrl:    "https://upstream.example",
			ChannelType:       constant.ChannelTypeAdvancedCustom,
			UpstreamModelName: "gpt-5",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AdvancedCustom: &dto.AdvancedCustomConfig{
					Routes: []dto.AdvancedCustomRoute{
						{
							IncomingPath: "/v1/responses",
							UpstreamPath: "/v1/responses",
							Converter:    "none",
						},
					},
				},
			},
			ChannelSetting: dto.ChannelSettings{
				CodexCompatEnabled: enabled,
				CodexClientVersion: version,
			},
		},
	}
}

func compatGinContext(headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c
}

func TestApplyCodexIdentityHeaders(t *testing.T) {
	t.Run("enabled sets UA and originator with defaults", func(t *testing.T) {
		header := http.Header{}
		applyCodexIdentityHeaders(compatGinContext(nil), &header, codexCompatRelayInfo(true, "0.146.0"))
		assert.True(t, strings.HasPrefix(header.Get("User-Agent"), "codex_cli_rs/0.146.0"))
		assert.Equal(t, "codex_cli_rs", header.Get("originator"))
	})

	t.Run("enabled passthrough client x-codex headers", func(t *testing.T) {
		header := http.Header{}
		c := compatGinContext(map[string]string{
			"session_id":        "client-session",
			"thread_id":         "client-thread",
			"X-Codex-Window-Id": "client-window",
		})
		applyCodexIdentityHeaders(c, &header, codexCompatRelayInfo(true, "0.146.0"))
		assert.Equal(t, "client-session", header.Get("session_id"))
		assert.Equal(t, "client-thread", header.Get("thread_id"))
		assert.Equal(t, "client-window", header.Get("x-codex-window-id"))
	})

	t.Run("enabled does not synthesize IDs when absent", func(t *testing.T) {
		header := http.Header{}
		applyCodexIdentityHeaders(compatGinContext(nil), &header, codexCompatRelayInfo(true, "0.146.0"))
		assert.Empty(t, header.Get("session_id"))
		assert.Empty(t, header.Get("thread_id"))
		assert.Empty(t, header.Get("x-codex-window-id"))
	})

	t.Run("custom client name and default version", func(t *testing.T) {
		header := http.Header{}
		info := codexCompatRelayInfo(true, "")
		info.ChannelSetting.CodexClientName = "codex-tui"
		applyCodexIdentityHeaders(compatGinContext(nil), &header, info)
		assert.True(t, strings.HasPrefix(header.Get("User-Agent"), "codex-tui/"+defaultCodexClientVersion))
		assert.Equal(t, "codex-tui", header.Get("originator"))
	})
}

func TestApplyClaudeCodeHeaders(t *testing.T) {
	t.Run("sets claude fingerprint headers", func(t *testing.T) {
		header := http.Header{}
		applyClaudeCodeHeaders(compatGinContext(nil), &header, codexCompatRelayInfo(false, ""))
		assert.Equal(t, claudeCodeUserAgent, header.Get("User-Agent"))
		assert.Equal(t, "cli", header.Get("x-app"))
		assert.Equal(t, claudeCodeBeta, header.Get("anthropic-beta"))
	})

	t.Run("preserves existing anthropic-beta and prepends marker", func(t *testing.T) {
		header := http.Header{}
		c := compatGinContext(map[string]string{"anthropic-beta": "some-beta-1"})
		applyClaudeCodeHeaders(c, &header, codexCompatRelayInfo(false, ""))
		assert.Equal(t, claudeCodeBeta+",some-beta-1", header.Get("anthropic-beta"))
	})

	t.Run("does not duplicate marker when already present", func(t *testing.T) {
		header := http.Header{}
		c := compatGinContext(map[string]string{"anthropic-beta": claudeCodeBeta + ",other"})
		applyClaudeCodeHeaders(c, &header, codexCompatRelayInfo(false, ""))
		assert.Equal(t, claudeCodeBeta+",other", header.Get("anthropic-beta"))
	})
}

func TestPrependClaudeCodeSystemPrompt(t *testing.T) {
	t.Run("string system becomes identity + original", func(t *testing.T) {
		req := &dto.ClaudeRequest{System: "Be helpful."}
		prependClaudeCodeSystemPrompt(req)
		blocks, ok := req.System.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, blocks, 2)
		assert.Equal(t, claudeCodeSystemIdentity, *blocks[0].Text)
		assert.Equal(t, "Be helpful.", *blocks[1].Text)
	})

	t.Run("array system gets identity first", func(t *testing.T) {
		orig := dto.ClaudeMediaMessage{Type: "text"}
		orig.SetText("Existing system.")
		req := &dto.ClaudeRequest{System: []dto.ClaudeMediaMessage{orig}}
		prependClaudeCodeSystemPrompt(req)
		blocks, ok := req.System.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, blocks, 2)
		assert.Equal(t, claudeCodeSystemIdentity, *blocks[0].Text)
		assert.Equal(t, "Existing system.", *blocks[1].Text)
	})

	t.Run("idempotent when identity already first", func(t *testing.T) {
		identity := dto.ClaudeMediaMessage{Type: "text"}
		identity.SetText(claudeCodeSystemIdentity)
		req := &dto.ClaudeRequest{System: []dto.ClaudeMediaMessage{identity}}
		prependClaudeCodeSystemPrompt(req)
		blocks, ok := req.System.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, blocks, 1)
		assert.Equal(t, claudeCodeSystemIdentity, *blocks[0].Text)
	})

	t.Run("nil system gets identity", func(t *testing.T) {
		req := &dto.ClaudeRequest{}
		prependClaudeCodeSystemPrompt(req)
		blocks, ok := req.System.([]dto.ClaudeMediaMessage)
		require.True(t, ok)
		require.Len(t, blocks, 1)
		assert.Equal(t, claudeCodeSystemIdentity, *blocks[0].Text)
	})
}
