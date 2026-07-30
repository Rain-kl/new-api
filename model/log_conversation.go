package model

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// ConversationRecord stores the raw request body (messages) for a consume log entry.
// This is an opt-in feature controlled by ConversationRecordEnabled setting.
//
// Tag notes (SQLite / glebarez + GORM v1.25):
//   - avoid `default:”` on strings — external DDL with DEFAULT ” makes AutoMigrate
//     fail with "failed to look up field … from DDL";
//   - prefer size:N over type:varchar(N) for portable string columns.
type ConversationRecord struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	LogId     int    `json:"log_id" gorm:"index;default:0"`
	UserId    int    `json:"user_id" gorm:"index;default:0"`
	RequestId string `json:"request_id" gorm:"size:64;index"`
	Content   string `json:"content" gorm:"type:text"`
	CreatedAt int64  `json:"created_at" gorm:"index;default:0"`
}

func init() {
	// Self-register: no edits to migrate lists or RecordConsumeLog body needed beyond stable hooks.
	RegisterLOGDBModel(&ConversationRecord{})
	RegisterAfterConsumeLogHook(saveConversationRecordAfterConsume)
}

func saveConversationRecordAfterConsume(c *gin.Context, log *Log, userId int, requestId string) {
	if !common.ConversationRecordEnabled || c == nil || log == nil {
		return
	}
	if storage, exists := c.Get(common.KeyBodyStorage); exists && storage != nil {
		if bs, ok := storage.(common.BodyStorage); ok {
			if bodyBytes, err := bs.Bytes(); err == nil && len(bodyBytes) > 0 {
				_ = SaveConversationRecord(log.Id, userId, requestId, string(bodyBytes))
			}
		}
	}
}

func SaveConversationRecord(logId int, userId int, requestId string, content string) error {
	if LOG_DB == nil {
		return fmt.Errorf("log database not initialized")
	}
	record := &ConversationRecord{
		LogId:     logId,
		UserId:    userId,
		RequestId: requestId,
		Content:   content,
		CreatedAt: common.GetTimestamp(),
	}
	return LOG_DB.Create(record).Error
}

func GetConversationByLogId(logId int) (*ConversationRecord, error) {
	if LOG_DB == nil {
		return nil, fmt.Errorf("log database not initialized")
	}
	var record ConversationRecord
	err := LOG_DB.Where("log_id = ?", logId).First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func DeleteOldConversationRecords(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if LOG_DB == nil {
		return 0, fmt.Errorf("log database not initialized")
	}
	var total int64 = 0
	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}
		result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&ConversationRecord{})
		if nil != result.Error {
			return total, result.Error
		}
		total += result.RowsAffected
		if result.RowsAffected < int64(limit) {
			break
		}
	}
	return total, nil
}
