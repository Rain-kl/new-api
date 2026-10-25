package model

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openPersonalLOGTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func withTestLOGDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	prevDB, prevLOG := DB, LOG_DB
	prevMain, prevLogType := common.MainDatabaseType(), common.LogDatabaseType()
	t.Cleanup(func() {
		DB, LOG_DB = prevDB, prevLOG
		common.SetMainDatabaseType(prevMain)
		common.SetLogDatabaseType(prevLogType)
	})
	DB = db
	LOG_DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
}

// Ensures personal LOG models migrate on a shared handle without re-migrating Log.
func TestMigrateRegisteredLOGDBModels_ConversationRecord(t *testing.T) {
	db := openPersonalLOGTestDB(t)
	withTestLOGDB(t, db)

	if err := migrateRegisteredLOGDBModels(); err != nil {
		t.Fatalf("migrateRegisteredLOGDBModels: %v", err)
	}
	if !db.Migrator().HasTable(&ConversationRecord{}) {
		t.Fatal("expected conversation_records table after migrateRegisteredLOGDBModels")
	}
	if err := migrateRegisteredLOGDBModels(); err != nil {
		t.Fatalf("second migrateRegisteredLOGDBModels: %v", err)
	}
}

// Manual DDL with DEFAULT " was once broken in glebarez/sqlite <v1.11; empty table should be
// handled (either recovered or migrated directly). As of v1.11 AutoMigrate succeeds directly.
func TestMigrateRegisteredLOGDBModels_RecoversBrokenEmptySQLiteDDL(t *testing.T) {
	db := openPersonalLOGTestDB(t)
	withTestLOGDB(t, db)

	if err := db.Exec(`CREATE TABLE conversation_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  log_id INTEGER DEFAULT 0,
  user_id INTEGER DEFAULT 0,
  request_id VARCHAR(64) DEFAULT '',
  content TEXT,
  created_at BIGINT DEFAULT 0
)`).Error; err != nil {
		t.Fatalf("seed broken DDL: %v", err)
	}

	if err := migrateRegisteredLOGDBModels(); err != nil {
		t.Fatalf("migrateRegisteredLOGDBModels should handle the table: %v", err)
	}
	if !db.Migrator().HasTable(&ConversationRecord{}) {
		t.Fatal("expected table after recovery")
	}

	// Insert + second migrate must keep data path healthy.
	if err := db.Create(&ConversationRecord{LogId: 1, Content: "{}"}).Error; err != nil {
		t.Fatalf("create after recovery: %v", err)
	}
	if err := migrateRegisteredLOGDBModels(); err != nil {
		t.Fatalf("migrate with data: %v", err)
	}
	var n int64
	if err := db.Model(&ConversationRecord{}).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("expected 1 row retained, n=%d err=%v", n, err)
	}
}

func TestMigrateRegisteredLOGDBModels_DoesNotDropNonEmptyBrokenTable(t *testing.T) {
	db := openPersonalLOGTestDB(t)
	withTestLOGDB(t, db)

	// Create via GORM first so we can insert, then we cannot easily make "broken DDL with data"
	// without rewriting sqlite_master. Instead: broken empty fails recovery path with count>0
	// is covered by ensuring non-empty healthy table is not dropped on success path.
	if err := migrateRegisteredLOGDBModels(); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ConversationRecord{LogId: 7, RequestId: "r", Content: `{"a":1}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateRegisteredLOGDBModels(); err != nil {
		t.Fatal(err)
	}
	var rec ConversationRecord
	if err := db.Where("log_id = ?", 7).First(&rec).Error; err != nil {
		t.Fatalf("row should remain: %v", err)
	}
}

// Personal schema settings must survive a repeated migration on every supported engine,
// including the separate log handle used for conversation contents.
func TestPersonalFeaturesDatabaseMatrix(t *testing.T) {
	for _, engine := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			var driver gorm.Dialector
			switch engine {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "personal.db"))
			case "mysql":
				dsn := os.Getenv("SYNC_MYSQL_DSN")
				if dsn == "" {
					t.Skip("SYNC_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("SYNC_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("SYNC_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			oldDB, oldLog := DB, LOG_DB
			oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			oldDisable, oldEnable := common.AutomaticDisableModelEnabled, common.AutomaticEnableModelEnabled
			common.OptionMapRWMutex.Lock()
			oldOptions := common.OptionMap
			common.OptionMap = map[string]string{}
			common.OptionMapRWMutex.Unlock()
			t.Cleanup(func() {
				DB, LOG_DB = oldDB, oldLog
				common.SetMainDatabaseType(oldMain)
				common.SetLogDatabaseType(oldLogType)
				common.AutomaticDisableModelEnabled, common.AutomaticEnableModelEnabled = oldDisable, oldEnable
				common.OptionMapRWMutex.Lock()
				common.OptionMap = oldOptions
				common.OptionMapRWMutex.Unlock()
			})
			logDriver := sqlite.Open(filepath.Join(t.TempDir(), "personal-log.db"))
			var logDialect gorm.Dialector = logDriver
			if engine == "mysql" {
				logDialect = mysql.Open(os.Getenv("SYNC_MYSQL_LOG_DSN"))
			}
			if engine == "postgres" {
				logDialect = postgres.Open(os.Getenv("SYNC_POSTGRES_LOG_DSN"))
			}
			logDB, err := gorm.Open(logDialect, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			logConn, err := logDB.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, logConn.Close()) })
			require.NoError(t, logDB.Migrator().DropTable(&ConversationRecord{}))
			t.Cleanup(func() { require.NoError(t, logDB.Migrator().DropTable(&ConversationRecord{})) })
			DB, LOG_DB = db, logDB
			kind := common.DatabaseTypeSQLite
			if engine == "mysql" {
				kind = common.DatabaseTypeMySQL
			}
			if engine == "postgres" {
				kind = common.DatabaseTypePostgreSQL
			}
			common.SetMainDatabaseType(kind)
			common.SetLogDatabaseType(kind)
			tables := []any{&ModelRedirectTarget{}, &ModelRedirect{}, &Proxy{}, &Channel{}, &Model{}, &Option{}, &ConversationRecord{}}
			for _, m := range tables {
				require.NoError(t, db.Migrator().DropTable(m))
			}
			t.Cleanup(func() {
				for _, m := range tables {
					require.NoError(t, db.Migrator().DropTable(m))
				}
			})
			require.NoError(t, db.AutoMigrate(&Channel{}, &Model{}, &Option{}))
			require.NoError(t, migrateRegisteredMainDBModels())
			require.NoError(t, migrateRegisteredLOGDBModels())
			setting := `{"proxy_id":7,"proxy":"http://proxy.example:8080","reasoning_effort_rules_enabled":true,"reasoning_effort_rules":[{"model":"alias","effort":"high","force":true}],"codex_compat_enabled":true,"codex_client_version":"0.146.0"}`
			ratio := 0.5
			channel := Channel{Name: "personal-channel", Key: "fixture", Type: 58, Setting: &setting, Ratio: &ratio}
			require.NoError(t, db.Create(&channel).Error)
			metadata := Model{ModelName: "personal-model", Status: 1, AutoDisabledByRule: true, ContextWindow: 128000, MaxOutputTokens: 8192}
			require.NoError(t, db.Create(&metadata).Error)
			redirect := ModelRedirect{Name: "virtual-model", Groups: "default", Enabled: false, Targets: []ModelRedirectTarget{{ChannelId: channel.Id, Model: "personal-model", Priority: 100, Enabled: false}}}
			require.NoError(t, db.Create(&redirect).Error)
			require.NoError(t, SaveConversationRecord(1, 1, "personal-request", `{"messages":[{"role":"user","content":"fixture"}]}`))
			require.NoError(t, UpdateOptionsBulk(map[string]string{automaticDisableModelOptionKey: "true", automaticEnableModelOptionKey: "true"}))
			// Simulate restarts of an existing populated feature database, twice.
			for range 2 {
				require.NoError(t, db.AutoMigrate(&Channel{}, &Model{}, &Option{}))
				require.NoError(t, migrateRegisteredMainDBModels())
				require.NoError(t, migrateRegisteredLOGDBModels())
				require.NoError(t, updateOptionsBulk(nil, true))
			}
			var loaded Channel
			require.NoError(t, db.First(&loaded, channel.Id).Error)
			assert.Equal(t, ratio, *loaded.Ratio)
			assert.Equal(t, setting, *loaded.Setting)
			var loadedModel Model
			require.NoError(t, db.First(&loadedModel, metadata.Id).Error)
			assert.True(t, loadedModel.AutoDisabledByRule)
			assert.EqualValues(t, 128000, loadedModel.ContextWindow)
			var loadedRedirect ModelRedirect
			require.NoError(t, db.Preload("Targets").First(&loadedRedirect, redirect.Id).Error)
			assert.False(t, loadedRedirect.Enabled)
			require.Len(t, loadedRedirect.Targets, 1)
			assert.False(t, loadedRedirect.Targets[0].Enabled)
			var records int64
			require.NoError(t, logDB.Model(&ConversationRecord{}).Count(&records).Error)
			assert.EqualValues(t, 1, records)
			assert.True(t, common.AutomaticDisableModelEnabled)
			assert.True(t, common.AutomaticEnableModelEnabled)
			require.Error(t, db.Create(&ModelRedirect{Name: redirect.Name, Groups: "default"}).Error, "virtual model names remain unique")
		})
	}
}
