package echo

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
)

func init() {
	// Self-register so relay.GetAdaptor / ChannelType2APIType need no personal case arms.
	channel.RegisterAdaptor(constant.APITypeEcho, func() channel.Adaptor {
		return &Adaptor{}
	})
	common.RegisterChannelAPIType(constant.ChannelTypeEcho, constant.APITypeEcho)
	// Echo must not be rewritten onto the Responses API path.
	common.RegisterSkipChatCompletionsToResponses(constant.ChannelTypeEcho)
}
