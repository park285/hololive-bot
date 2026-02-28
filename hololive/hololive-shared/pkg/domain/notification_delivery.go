package domain

import (
	"database/sql"
	"time"
)

// DeliveryOutboxKind: outbox 항목 종류
type DeliveryOutboxKind string

const (
	DeliveryKindMajorEventWeekly  DeliveryOutboxKind = "MAJOR_EVENT_WEEKLY"
	DeliveryKindMajorEventMonthly DeliveryOutboxKind = "MAJOR_EVENT_MONTHLY"
	DeliveryKindMemberNewsWeekly  DeliveryOutboxKind = "MEMBER_NEWS_WEEKLY"
	DeliveryKindMemberNewsMonthly DeliveryOutboxKind = "MEMBER_NEWS_MONTHLY"
)

// DeliveryOutboxStatus: outbox 항목 상태
type DeliveryOutboxStatus string

const (
	DeliveryStatusPending DeliveryOutboxStatus = "PENDING"
	DeliveryStatusSent    DeliveryOutboxStatus = "SENT"
	DeliveryStatusFailed  DeliveryOutboxStatus = "FAILED"
)

// NotificationDeliveryOutbox: outbox 테이블 도메인 모델
type NotificationDeliveryOutbox struct {
	ID            int64                `gorm:"primaryKey;autoIncrement"`
	Kind          DeliveryOutboxKind   `gorm:"column:kind;type:varchar(30);not null"`
	PeriodKey     string               `gorm:"column:period_key;type:varchar(20);not null"`
	RoomID        string               `gorm:"column:room_id;type:varchar(100);not null"`
	ContentID     string               `gorm:"column:content_id;type:varchar(200);not null"`
	Payload       string               `gorm:"column:payload;type:jsonb;not null;default:'{}'"`
	Status        DeliveryOutboxStatus `gorm:"column:status;type:varchar(20);not null;default:'PENDING'"`
	AttemptCount  int                  `gorm:"column:attempt_count;not null;default:0"`
	NextAttemptAt time.Time            `gorm:"column:next_attempt_at;not null;default:now()"`
	CreatedAt     time.Time            `gorm:"column:created_at;not null;default:now()"`
	LockedAt      sql.NullTime         `gorm:"column:locked_at"`
	SentAt        sql.NullTime         `gorm:"column:sent_at"`
	Error         sql.NullString       `gorm:"column:error"`
}

func (NotificationDeliveryOutbox) TableName() string {
	return "notification_delivery_outbox"
}
