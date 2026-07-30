package common

import "sync"

// Extra channel-type → API-type mappings for site-local patches.
// Keeps common.ChannelType2APIType free of personal case arms that conflict on merge.

var (
	extraChannelAPITypeMu sync.RWMutex
	extraChannelAPITypes  = map[int]int{}
)

// RegisterChannelAPIType registers a personal channel type mapping. Call from init().
func RegisterChannelAPIType(channelType int, apiType int) {
	extraChannelAPITypeMu.Lock()
	extraChannelAPITypes[channelType] = apiType
	extraChannelAPITypeMu.Unlock()
}

func lookupExtraChannelAPIType(channelType int) (int, bool) {
	extraChannelAPITypeMu.RLock()
	apiType, ok := extraChannelAPITypes[channelType]
	extraChannelAPITypeMu.RUnlock()
	return apiType, ok
}
