package analytics

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type PostSendCount struct {
	OutboxKind            domain.OutboxKind `gorm:"column:outbox_kind"`
	AlarmType             domain.AlarmType  `gorm:"column:alarm_type"`
	ChannelID             string            `gorm:"column:channel_id"`
	PostID                string            `gorm:"column:post_id"`
	ContentID             string            `gorm:"column:content_id"`
	ActualPublishedAt     *time.Time        `gorm:"column:actual_published_at"`
	DetectedAt            *time.Time        `gorm:"column:detected_at"`
	AlarmSentAt           *time.Time        `gorm:"column:alarm_sent_at"`
	AlarmLatencyMillis    *int64            `gorm:"column:alarm_latency_millis"`
	AlarmLatencyExceeded  *bool             `gorm:"-"`
	FirstEventAt          *time.Time        `gorm:"column:first_event_at"`
	LastEventAt           *time.Time        `gorm:"column:last_event_at"`
	FirstSuccessAt        *time.Time        `gorm:"column:first_success_at"`
	LastSuccessAt         *time.Time        `gorm:"column:last_success_at"`
	OutboxCount           int64             `gorm:"column:outbox_count"`
	SuccessSendCount      int64             `gorm:"column:success_send_count"`
	SuccessRoomCount      int64             `gorm:"column:success_room_count"`
	DuplicateSuccessCount int64             `gorm:"column:duplicate_success_count"`
	FailedAttemptCount    int64             `gorm:"column:failed_attempt_count"`
}

type ChannelPostDeliverySummary struct {
	ChannelID                  string     `json:"channel_id"`
	EarliestObservedAt         *time.Time `json:"earliest_observed_at,omitempty"`
	LatestObservedAt           *time.Time `json:"latest_observed_at,omitempty"`
	DetectedPostCount          int64      `json:"detected_post_count"`
	AlarmSentPostCount         int64      `json:"alarm_sent_post_count"`
	SuccessPostCount           int64      `json:"success_post_count"`
	FailedPostCount            int64      `json:"failed_post_count"`
	DetectedUnsentPostCount    int64      `json:"detected_unsent_post_count"`
	CommunityDetectedPostCount int64      `json:"community_detected_post_count"`
	ShortsDetectedPostCount    int64      `json:"shorts_detected_post_count"`
}

type PostDeliveryPathUsage struct {
	OutboxKind         domain.OutboxKind `gorm:"column:outbox_kind"`
	AlarmType          domain.AlarmType  `gorm:"column:alarm_type"`
	ChannelID          string            `gorm:"column:channel_id"`
	PostID             string            `gorm:"column:post_id"`
	ContentID          string            `gorm:"column:content_id"`
	DeliveryPath       string            `gorm:"column:delivery_path"`
	ActualPublishedAt  *time.Time        `gorm:"column:actual_published_at"`
	DetectedAt         *time.Time        `gorm:"column:detected_at"`
	FirstEventAt       *time.Time        `gorm:"column:first_event_at"`
	LastEventAt        *time.Time        `gorm:"column:last_event_at"`
	FirstSuccessAt     *time.Time        `gorm:"column:first_success_at"`
	LastSuccessAt      *time.Time        `gorm:"column:last_success_at"`
	SuccessSendCount   int64             `gorm:"column:success_send_count"`
	SuccessRoomCount   int64             `gorm:"column:success_room_count"`
	FailedAttemptCount int64             `gorm:"column:failed_attempt_count"`
}

type PostLatencyPeriod struct {
	Label   string
	StartAt time.Time
	EndAt   time.Time
}

type PostLatencyPeriodSummary struct {
	Label                      string
	StartAt                    time.Time
	EndAt                      time.Time
	TotalPostCount             int64
	AlarmSentPostCount         int64
	PendingPostCount           int64
	LatencyMeasuredPostCount   int64
	WithinTargetPostCount      int64
	ExceededPostCount          int64
	CommunityPostCount         int64
	CommunityExceededPostCount int64
	ShortsPostCount            int64
	ShortsExceededPostCount    int64
	AverageLatencyMillis       *int64
	P95LatencyMillis           *int64
	MaxLatencyMillis           *int64
}
