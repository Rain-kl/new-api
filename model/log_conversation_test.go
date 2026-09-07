package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordErrorLog_SaveConversationRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openPersonalLOGTestDB(t)
	withTestLOGDB(t, db)

	err := db.AutoMigrate(&Log{})
	require.NoError(t, err)
	err = migrateRegisteredLOGDBModels()
	require.NoError(t, err)

	common.ConversationRecordEnabled = true
	defer func() {
		common.ConversationRecordEnabled = false
	}()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(common.RequestIdKey, "req-error-test-123")

	reqBody := `{"model":"test-model","messages":[{"role":"user","content":"hello error"}]}`
	bodyStorage, err := common.CreateBodyStorage([]byte(reqBody))
	require.NoError(t, err)
	c.Set(common.KeyBodyStorage, bodyStorage)

	other := NewLogOther()
	other.SetPublic("error_code", "invalid_parameter")
	RecordErrorLog(c, 1, 10, "test-model", "test-token", "invalid parameter error", 5, 2, false, "default", other)

	// Fetch saved log entry
	var log Log
	err = LOG_DB.Where("request_id = ?", "req-error-test-123").First(&log).Error
	require.NoError(t, err)
	assert.Equal(t, LogTypeError, log.Type)

	// Fetch conversation record
	record, err := GetConversationByLogId(log.Id)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, log.Id, record.LogId)
	assert.Equal(t, 1, record.UserId)
	assert.Equal(t, "req-error-test-123", record.RequestId)
	assert.Equal(t, reqBody, record.Content)
}
