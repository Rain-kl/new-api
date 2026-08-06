package openai

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
			UpstreamModelName: "o3-high",
		},
		ReasoningEffort:            "max",
		ReasoningEffortFromChannel: true,
	}
	req := &dto.GeneralOpenAIRequest{
		Model:           "o3-high",
		ReasoningEffort: "max",
	}

	a := &Adaptor{}
	out, err := a.ConvertOpenAIRequest(c, info, req)
	require.NoError(t, err)
	converted, ok := out.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)

	assert.Equal(t, "max", converted.ReasoningEffort)
	assert.Equal(t, "o3", converted.Model)
	assert.Equal(t, "o3", info.UpstreamModelName)
	assert.Equal(t, "max", info.ReasoningEffort)
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
			UpstreamModelName: "o3-high",
		},
	}
	req := &dto.GeneralOpenAIRequest{
		Model: "o3-high",
	}

	a := &Adaptor{}
	out, err := a.ConvertOpenAIRequest(c, info, req)
	require.NoError(t, err)
	converted, ok := out.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)

	assert.Equal(t, "high", converted.ReasoningEffort)
	assert.Equal(t, "o3", converted.Model)
	assert.Equal(t, "o3", info.UpstreamModelName)
	assert.Equal(t, "high", info.ReasoningEffort)
}

func TestConvertOpenAIResponsesRequest_SuffixDoesNotOverwriteChannelEffort(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ReasoningEffort:            "max",
		ReasoningEffortFromChannel: true,
	}
	req := dto.OpenAIResponsesRequest{
		Model: "o3-high",
		Reasoning: &dto.Reasoning{
			Effort: "max",
		},
	}

	a := &Adaptor{}
	out, err := a.ConvertOpenAIResponsesRequest(nil, info, req)
	require.NoError(t, err)
	converted, ok := out.(dto.OpenAIResponsesRequest)
	require.True(t, ok)

	require.NotNil(t, converted.Reasoning)
	assert.Equal(t, "max", converted.Reasoning.Effort)
	assert.Equal(t, "o3", converted.Model)
	assert.Equal(t, "max", info.ReasoningEffort)
}
