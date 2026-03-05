package domain

import "time"

// YouTubeNotificationDelivery: room 단위 알림 전달 상태
type YouTubeNotificationDelivery struct {
	ID            int64        `gorm:"primaryKey;autoIncrement" json:"id"`
	OutboxID      int64        `gorm:"not null;index:idx_ynd_outbox_room,unique" json:"outbox_id"`
	RoomID        string       `gorm:"size:100;not null;index:idx_ynd_outbox_room,unique" json:"room_id"`
	Status        OutboxStatus `gorm:"size:20;not null;default:'PENDING';index:idx_ynd_pending_next" json:"status"`
	AttemptCount  int          `gorm:"not null;default:0" json:"attempt_count"`
	NextAttemptAt time.Time    `gorm:"not null;default:NOW();index:idx_ynd_pending_next" json:"next_attempt_at"`
	CreatedAt     time.Time    `gorm:"autoCreateTime;index:idx_ynd_pending_next" json:"created_at"`
	LockedAt      *time.Time   `json:"locked_at,omitempty"`
	SentAt        *time.Time   `json:"sent_at,omitempty"`
	Error         string       `gorm:"type:text" json:"error,omitempty"`
}

func (YouTubeNotificationDelivery) TableName() string {
	return "youtube_notification_delivery"
}
