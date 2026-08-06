package xai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIRequest_SuffixDoesNotOverwriteChannelEffort(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "grok-3-mini-high",
		},
		ReasoningEffort:            "low",
		ReasoningEffortFromChannel: true,
	}
	req := &dto.GeneralOpenAIRequest{
		Model:           "grok-3-mini-high",
		ReasoningEffort: "low",
	}

	a := &Adaptor{}
	out, err := a.ConvertOpenAIRequest(c, info, req)
	require.NoError(t, err)
	converted, ok := out.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)

	assert.Equal(t, "low", converted.ReasoningEffort)
	assert.Equal(t, "grok-3-mini", converted.Model)
	assert.Equal(t, "grok-3-mini", info.UpstreamModelName)
	assert.Equal(t, "low", info.ReasoningEffort)
	assert.True(t, info.ReasoningEffortFromChannel)
}

func TestConvertOpenAIRequest_SuffixAppliesWhenNotFromChannel(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "grok-3-mini-high",
		},
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "grok-3-mini-high",
	}

	a := &Adaptor{}
	out, err := a.ConvertOpenAIRequest(c, info, req)
	require.NoError(t, err)
	converted, ok := out.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)

	assert.Equal(t, "high", converted.ReasoningEffort)
	assert.Equal(t, "grok-3-mini", converted.Model)
	assert.Equal(t, "grok-3-mini", info.UpstreamModelName)
	assert.Equal(t, "high", info.ReasoningEffort)
}
