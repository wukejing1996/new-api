package model

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

type RegistrationIPDetections struct {
	Proxy      *bool `json:"proxy"`
	VPN        *bool `json:"vpn"`
	Tor        *bool `json:"tor"`
	Hosting    *bool `json:"hosting"`
	Scraper    *bool `json:"scraper"`
	Risk       *int  `json:"risk"`
	Confidence *int  `json:"confidence"`
}

type RegistrationIPResult struct {
	Network struct {
		Type *string `json:"type"`
	} `json:"network"`
	Detections RegistrationIPDetections `json:"detections"`
}

type RegistrationRiskAssessment struct {
	Restricted bool                  `json:"restricted"`
	Incomplete bool                  `json:"incomplete"`
	Reasons    []string              `json:"reasons"`
	IP         *RegistrationIPResult `json:"ip_result,omitempty"`
	Disposable *bool                 `json:"disposable,omitempty"`
}

// An immutable assessment and processing snapshot, without credentials.
// Association records describe the trigger, not a new lookup for the earlier user.
type RegistrationRiskRecord struct {
	Source          string                         `json:"source"`
	Assessment      RegistrationRiskAssessment     `json:"assessment"`
	Policy          setting.RegistrationRiskConfig `json:"policy"`
	Outcome         string                         `json:"outcome"`
	OriginalGroup   string                         `json:"original_group"`
	FinalGroup      *string                        `json:"final_group"`
	AccountCreated  bool                           `json:"account_created"`
	GroupApplied    bool                           `json:"group_applied"`
	CacheSynced     bool                           `json:"cache_synced"`
	RelatedUserIDs  []int                          `json:"related_user_ids"`
	TriggerUserID   int                            `json:"trigger_user_id"`
	ProcessingError string                         `json:"processing_error,omitempty"`
}

func (record RegistrationRiskRecord) Value() (driver.Value, error) {
	data, err := common.Marshal(record)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (record *RegistrationRiskRecord) Scan(value any) error {
	var data []byte
	switch value := value.(type) {
	case nil:
		*record = RegistrationRiskRecord{}
		return nil
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		return fmt.Errorf("unsupported registration risk record database type %T", value)
	}
	var decoded RegistrationRiskRecord
	if len(data) > 0 {
		if err := common.Unmarshal(data, &decoded); err != nil {
			return err
		}
	}
	*record = decoded
	return nil
}

// Stored in the primary database; generic audit/self and usage-log APIs never
// query this table. No user foreign key: failed creates and deleted users retain history.
type RegistrationRiskLog struct {
	Id        int                    `json:"id"`
	EventId   string                 `json:"event_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId    int                    `json:"user_id" gorm:"index:idx_cr_reg_risk_user_time,priority:1"`
	Username  string                 `json:"username" gorm:"type:varchar(64);index"`
	CreatedAt int64                  `json:"created_at" gorm:"type:bigint;index;index:idx_cr_reg_risk_user_time,priority:2"`
	Ip        string                 `json:"ip" gorm:"type:varchar(64)"`
	UserAgent string                 `json:"user_agent" gorm:"type:varchar(512)"`
	RequestId string                 `json:"request_id" gorm:"type:varchar(64);index"`
	Success   bool                   `json:"success"`
	Details   RegistrationRiskRecord `json:"details" gorm:"type:text"`
}

func (RegistrationRiskLog) TableName() string { return "registration_risk_logs" }

type RegistrationRiskLogFilter struct {
	UserId         int
	Username       string
	RequestId      string
	StartTimestamp int64
	EndTimestamp   int64
	Success        *bool
}

func RecordRegistrationRiskLog(c *gin.Context, entry RegistrationRiskLog) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 3*time.Second)
	defer cancel()
	entry.EventId = common.NewRequestId()
	entry.CreatedAt = common.GetTimestamp()
	entry.RequestId = c.GetString(common.RequestIdKey)
	if entry.RequestId == "" {
		entry.RequestId = common.NewRequestId()
	}
	entry.Ip = c.ClientIP()
	ua := []rune(c.Request.UserAgent())
	entry.UserAgent = string(ua[:min(len(ua), 512)])
	if DB == nil {
		logger.LogError(ctx, fmt.Sprintf("CostRouter registration risk record write failed (request_id=%s): database unavailable", entry.RequestId))
		return
	}
	if err := DB.WithContext(ctx).Create(&entry).Error; err != nil {
		logger.LogError(ctx, fmt.Sprintf("CostRouter registration risk record write failed (request_id=%s): %v", entry.RequestId, err))
	}
}

func GetRegistrationRiskLogs(filter RegistrationRiskLogFilter, start, limit int) ([]RegistrationRiskLog, int64, error) {
	query := DB.Model(&RegistrationRiskLog{})
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.Username != "" {
		query = query.Where("username = ?", filter.Username)
	}
	if filter.RequestId != "" {
		query = query.Where("request_id = ?", filter.RequestId)
	}
	if filter.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("created_at <= ?", filter.EndTimestamp)
	}
	if filter.Success != nil {
		query = query.Where("success = ?", *filter.Success)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]RegistrationRiskLog, 0)
	if err := query.Order("created_at DESC").Order("id DESC").Offset(start).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
