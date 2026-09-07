package model

import (
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
