package model

import (
	"sync"

	"github.com/gin-gonic/gin"
)

// AfterConsumeLogHook runs after a consume log row is created.
// Personal features (e.g. conversation recording) register here so model/log.go
// only needs a stable one-line call site that rarely conflicts on merge.
type AfterConsumeLogHook func(c *gin.Context, log *Log, userId int, requestId string)

var (
	afterConsumeLogMu    sync.RWMutex
	afterConsumeLogHooks []AfterConsumeLogHook
)

// RegisterAfterConsumeLogHook appends a hook. Safe to call from init().
func RegisterAfterConsumeLogHook(hook AfterConsumeLogHook) {
	if hook == nil {
		return
	}
	afterConsumeLogMu.Lock()
	afterConsumeLogHooks = append(afterConsumeLogHooks, hook)
	afterConsumeLogMu.Unlock()
}

func runAfterConsumeLogHooks(c *gin.Context, log *Log, userId int, requestId string) {
	afterConsumeLogMu.RLock()
	hooks := afterConsumeLogHooks
	afterConsumeLogMu.RUnlock()
	for _, hook := range hooks {
		hook(c, log, userId, requestId)
	}
}

// Extra models migrated into LOG_DB (personal tables). Registered via init().
var (
	extraLOGDBModelMu sync.Mutex
	extraLOGDBModels  []any
)

// RegisterLOGDBModel adds a model to LOG_DB AutoMigrate. Call from init().
func RegisterLOGDBModel(m any) {
	if m == nil {
		return
	}
	extraLOGDBModelMu.Lock()
	extraLOGDBModels = append(extraLOGDBModels, m)
	extraLOGDBModelMu.Unlock()
}

func logDBMigrateModels() []any {
	extraLOGDBModelMu.Lock()
	defer extraLOGDBModelMu.Unlock()
	models := make([]any, 0, 1+len(extraLOGDBModels))
	models = append(models, &Log{})
	models = append(models, extraLOGDBModels...)
	return models
}
