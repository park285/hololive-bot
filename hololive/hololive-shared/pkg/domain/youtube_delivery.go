package domain

import "time"

type YouTubeNotificationDelivery struct {
	ID            int64        `db:"id" json:"id"`
	OutboxID      int64        `db:"outbox_id" json:"outbox_id"`
	RoomID        string       `db:"room_id" json:"room_id"`
	Status        OutboxStatus `db:"status" json:"status"`
	AttemptCount  int          `db:"attempt_count" json:"attempt_count"`
	NextAttemptAt time.Time    `db:"next_attempt_at" json:"next_attempt_at"`
	CreatedAt     time.Time    `db:"created_at" json:"created_at"`
	LockedAt      *time.Time   `json:"locked_at,omitempty"`
	LockedBy      *string      `json:"locked_by,omitempty"`
	SentAt        *time.Time   `json:"sent_at,omitempty"`
	Error         string       `db:"error" json:"error,omitempty"`
	RowVersion    int64        `db:"row_version" json:"row_version"`
}
