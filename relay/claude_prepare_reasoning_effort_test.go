package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareClaudeRequest_FromChannelStillSanitizesOpus47Sampling(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	channelEffort := []byte(`{"effort":"max"}`)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-opus-4-7-high",
		},
		ReasoningEffort:            "max",
		ReasoningEffortFromChannel: true,
	}
	req := &dto.ClaudeRequest{
		Model:        "claude-opus-4-7-high",
		OutputConfig: channelEffort,
		Temperature:  common.GetPointer(0.7),
		TopP:         common.GetPointer(0.9),
		TopK:         common.GetPointer(40),
		Thinking:     &dto.Thinking{Type: "adaptive"},
	}

	prepareClaudeRequest(c, info, req)

	assert.Equal(t, "claude-opus-4-7", req.Model)
	assert.Equal(t, "claude-opus-4-7", info.UpstreamModelName)
	assert.JSONEq(t, `{"effort":"max"}`, string(req.OutputConfig))
	require.NotNil(t, req.Thinking)
	assert.Equal(t, "adaptive", req.Thinking.Type)
	assert.Equal(t, "summarized", req.Thinking.Display)
	assert.Nil(t, req.Temperature)
	assert.Nil(t, req.TopP)
	assert.Nil(t, req.TopK)
	assert.Equal(t, "max", info.ReasoningEffort)
}

func TestPrepareClaudeRequest_SuffixAppliesWhenNotFromChannel(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-opus-4-7-high",
		},
	}
	req := &dto.ClaudeRequest{
		Model:       "claude-opus-4-7-high",
		Temperature: common.GetPointer(0.5),
	}

	prepareClaudeRequest(c, info, req)

	assert.Equal(t, "claude-opus-4-7", req.Model)
	require.NotNil(t, req.Thinking)
	assert.Equal(t, "adaptive", req.Thinking.Type)
	assert.Equal(t, "summarized", req.Thinking.Display)
	assert.JSONEq(t, `{"effort":"high"}`, string(req.OutputConfig))
	assert.Nil(t, req.Temperature)
	assert.Nil(t, req.TopP)
	assert.Nil(t, req.TopK)
}
