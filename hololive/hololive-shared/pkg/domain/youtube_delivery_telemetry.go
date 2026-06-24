package domain

import "time"

// YouTubeNotificationDeliveryTelemetry는 커뮤니티/쇼츠 발송 감사 이벤트의 영속 버퍼입니다.
type YouTubeNotificationDeliveryTelemetry struct {
	ID                 int64      `db:"id" json:"id"`
	DeliveryID         int64      `db:"delivery_id" json:"delivery_id"`
	AttemptOrdinal     int        `db:"attempt_ordinal" json:"attempt_ordinal"`
	OutboxID           int64      `db:"outbox_id" json:"outbox_id"`
	ChannelID          string     `db:"channel_id" json:"channel_id"`
	ContentID          string     `db:"content_id" json:"content_id"`
	PostID             string     `db:"post_id" json:"post_id"`
	RoomID             string     `db:"room_id" json:"room_id"`
	AlarmType          AlarmType  `db:"alarm_type" json:"alarm_type"`
	ActualPublishedAt  *time.Time `json:"actual_published_at,omitempty"`
	AlarmSentAt        *time.Time `json:"alarm_sent_at,omitempty"`
	AlarmLatencyMillis *int64     `json:"alarm_latency_millis,omitempty"`
	DetectedAt         *time.Time `json:"detected_at,omitempty"`
	DedupeKey          string     `db:"dedupe_key" json:"dedupe_key"`
	DeliveryPath       string     `db:"delivery_path" json:"delivery_path"`
	DeliveryMode       string     `db:"delivery_mode" json:"delivery_mode"`
	SendResult         string     `db:"send_result" json:"send_result"`
	FailureReason      string     `db:"failure_reason" json:"failure_reason,omitempty"`
	AttemptStartedAt   *time.Time `json:"attempt_started_at,omitempty"`
	AttemptFinishedAt  *time.Time `json:"attempt_finished_at,omitempty"`
	EventAt            time.Time  `db:"event_at" json:"event_at"`
	NextAttemptAt      time.Time  `db:"next_attempt_at" json:"next_attempt_at"`
	CreatedAt          time.Time  `db:"created_at" json:"created_at"`
	LockedAt           *time.Time `json:"locked_at,omitempty"`
	LoggedAt           *time.Time `json:"logged_at,omitempty"`
	Error              string     `db:"error" json:"error,omitempty"`
}

func (YouTubeNotificationDeliveryTelemetry) TableName() string {
	return "youtube_notification_delivery_telemetry"
}
