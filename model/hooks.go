package model

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
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

// Extra models migrated into LOG_DB (personal SQL tables). Registered via init().
// Not applied to ClickHouse log backends (those only use the dedicated logs table DDL).
var (
	extraLOGDBModelMu sync.Mutex
	extraLOGDBModels  []any
)

// RegisterLOGDBModel adds a model to personal LOG_DB AutoMigrate. Call from init().
func RegisterLOGDBModel(m any) {
	if m == nil {
		return
	}
	extraLOGDBModelMu.Lock()
	extraLOGDBModels = append(extraLOGDBModels, m)
	extraLOGDBModelMu.Unlock()
}

func registeredLOGDBModels() []any {
	extraLOGDBModelMu.Lock()
	defer extraLOGDBModelMu.Unlock()
	if len(extraLOGDBModels) == 0 {
		return nil
	}
	out := make([]any, len(extraLOGDBModels))
	copy(out, extraLOGDBModels)
	return out
}

// migrateRegisteredLOGDBModels AutoMigrates only personal/extra LOG tables on LOG_DB.
//
// Use when LOG_DB shares the main database (no LOG_SQL_DSN): Log{} is already
// migrated by migrateDB(); only registered personal tables are needed.
// Also invoked after dedicated LOG_SQL_DSN migrate of Log{} so personal tables
// land on the log database without editing upstream model lists.
//
// No-op when LOG_DB is nil, nothing is registered, or log backend is ClickHouse.
//
// SQLite note: tables created outside GORM (or with DEFAULT ”) can make the
// glebarez migrator fail with "failed to look up field … from DDL". For empty
// personal tables we drop and recreate once so startup recovers automatically.
func migrateRegisteredLOGDBModels() error {
	if LOG_DB == nil {
		return nil
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return nil
	}
	models := registeredLOGDBModels()
	if len(models) == 0 {
		return nil
	}
	for _, m := range models {
		if err := LOG_DB.AutoMigrate(m); err != nil {
			if err2 := recoverEmptyPersonalLOGTableMigrate(m, err); err2 != nil {
				return err2
			}
		}
	}
	return nil
}

func isSQLiteDDLFieldLookupError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "failed to look up field") && strings.Contains(msg, "from DDL")
}

// recoverEmptyPersonalLOGTableMigrate drops an empty broken personal table and retries AutoMigrate.
func recoverEmptyPersonalLOGTableMigrate(m any, migrateErr error) error {
	if !isSQLiteDDLFieldLookupError(migrateErr) {
		return migrateErr
	}
	if !LOG_DB.Migrator().HasTable(m) {
		return migrateErr
	}
	var count int64
	if err := LOG_DB.Model(m).Count(&count).Error; err != nil {
		// Broken schema may even fail Count; still allow drop if table exists and empty-ish.
		count = -1
	}
	if count > 0 {
		return migrateErr
	}
	common.SysLog("personal LOG table has incompatible SQLite DDL; recreating empty table")
	if err := LOG_DB.Migrator().DropTable(m); err != nil {
		return err
	}
	return LOG_DB.AutoMigrate(m)
}
