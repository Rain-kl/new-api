package common

import "sync"

// Channel types that must never be upgraded ChatCompletions → Responses.
// Personal channels (e.g. Echo) register here from init() instead of patching
// every relay handler condition.

var (
	skipChatToResponsesMu sync.RWMutex
	skipChatToResponses   = map[int]bool{}
)

// RegisterSkipChatCompletionsToResponses marks a channel type as exempt from
// the global chat→responses conversion policy.
func RegisterSkipChatCompletionsToResponses(channelType int) {
	skipChatToResponsesMu.Lock()
	skipChatToResponses[channelType] = true
	skipChatToResponsesMu.Unlock()
}

// ShouldSkipChatCompletionsToResponses reports whether conversion must be skipped.
func ShouldSkipChatCompletionsToResponses(channelType int) bool {
	skipChatToResponsesMu.RLock()
	skip := skipChatToResponses[channelType]
	skipChatToResponsesMu.RUnlock()
	return skip
}
