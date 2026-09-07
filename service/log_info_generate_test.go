package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoIncludesChannelRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		ChannelMeta:       &relaycommon.ChannelMeta{},
		StartTime:         time.Now(),
		FirstResponseTime: time.Now(),
		PriceData: hosttypes.PriceData{
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 2, ChannelRatio: 0.5},
		},
	}

	other := GenerateTextOtherInfo(ctx, relayInfo, 1, 2, 1, 0, 0, 0, 0)
	require.Equal(t, 0.5, other.Snapshot()["channel_ratio"])
}
