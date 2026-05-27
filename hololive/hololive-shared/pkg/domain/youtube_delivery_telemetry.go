package domain

import "time"

// YouTubeNotificationDeliveryTelemetry는 커뮤니티/쇼츠 발송 감사 이벤트의 영속 버퍼입니다.
type YouTubeNotificationDeliveryTelemetry struct {
	ID                          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	DeliveryID                  int64      `gorm:"not null;uniqueIndex:idx_ydt_delivery_attempt" json:"delivery_id"`
	AttemptOrdinal              int        `gorm:"not null;uniqueIndex:idx_ydt_delivery_attempt" json:"attempt_ordinal"`
	OutboxID                    int64      `gorm:"not null;index:idx_ydt_pending_next" json:"outbox_id"`
	ChannelID                   string     `gorm:"size:50;not null" json:"channel_id"`
	ContentID                   string     `gorm:"size:50;not null" json:"content_id"`
	PostID                      string     `gorm:"size:50;not null;index:idx_ydt_post_event" json:"post_id"`
	RoomID                      string     `gorm:"size:100;not null" json:"room_id"`
	AlarmType                   AlarmType  `gorm:"size:20;not null" json:"alarm_type"`
	ActualPublishedAt           *time.Time `json:"actual_published_at,omitempty"`
	AlarmSentAt                 *time.Time `json:"alarm_sent_at,omitempty"`
	AlarmLatencyMillis          *int64     `json:"alarm_latency_millis,omitempty"`
	DetectedAt                  *time.Time `json:"detected_at,omitempty"`
	ObservationStatus           string     `gorm:"size:40;not null;default:'unclassified';index:idx_ydt_observation_status_event,priority:1" json:"observation_status"`
	ObservationRuntimeName      string     `gorm:"size:50;index:idx_ydt_observation_window_event,priority:1" json:"observation_runtime_name,omitempty"`
	ObservationBigBangCutoverAt *time.Time `gorm:"column:observation_bigbang_cutover_at;index:idx_ydt_observation_window_event,priority:2" json:"observation_bigbang_cutover_at,omitempty"`
	ObservationStartedAt        *time.Time `json:"observation_started_at,omitempty"`
	ObservationEndedAt          *time.Time `json:"observation_ended_at,omitempty"`
	DedupeKey                   string     `gorm:"size:200;not null" json:"dedupe_key"`
	DeliveryPath                string     `gorm:"size:100;not null" json:"delivery_path"`
	DeliveryMode                string     `gorm:"size:20;not null" json:"delivery_mode"`
	SendResult                  string     `gorm:"size:20;not null" json:"send_result"`
	FailureReason               string     `gorm:"size:100" json:"failure_reason,omitempty"`
	AttemptStartedAt            *time.Time `json:"attempt_started_at,omitempty"`
	AttemptFinishedAt           *time.Time `json:"attempt_finished_at,omitempty"`
	EventAt                     time.Time  `gorm:"not null" json:"event_at"`
	NextAttemptAt               time.Time  `gorm:"not null;default:NOW();index:idx_ydt_pending_next" json:"next_attempt_at"`
	CreatedAt                   time.Time  `gorm:"autoCreateTime" json:"created_at"`
	LockedAt                    *time.Time `json:"locked_at,omitempty"`
	LoggedAt                    *time.Time `json:"logged_at,omitempty"`
	Error                       string     `gorm:"type:text" json:"error,omitempty"`
}

func (YouTubeNotificationDeliveryTelemetry) TableName() string {
	return "youtube_notification_delivery_telemetry"
}
