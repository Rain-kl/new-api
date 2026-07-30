package channel

import "sync"

// Optional adaptor registry for site-local / personal channel patches.
// Upstream GetAdaptor switch stays untouched; personal packages register via init().

var (
	adaptorMu        sync.RWMutex
	adaptorFactories = map[int]func() Adaptor{}
)

// RegisterAdaptor registers a factory for an API type that is not handled by
// the upstream switch in relay.GetAdaptor. Safe to call from init().
func RegisterAdaptor(apiType int, factory func() Adaptor) {
	if factory == nil {
		return
	}
	adaptorMu.Lock()
	adaptorFactories[apiType] = factory
	adaptorMu.Unlock()
}

// GetRegisteredAdaptor returns a new adaptor instance if one was registered.
func GetRegisteredAdaptor(apiType int) Adaptor {
	adaptorMu.RLock()
	factory := adaptorFactories[apiType]
	adaptorMu.RUnlock()
	if factory == nil {
		return nil
	}
	return factory()
}
