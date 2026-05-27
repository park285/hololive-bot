package delivery

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

type PostDelaySource = timeline.PostDelaySource
type PostInternalDelayCause = timeline.PostInternalDelayCause
type PostLatencyClassificationStatus = timeline.PostLatencyClassificationStatus
type PostLatencyClassificationEvidenceKey = timeline.PostLatencyClassificationEvidenceKey
type PostLatencyClassificationEvidence = timeline.PostLatencyClassificationEvidence
type PostLatencyClassificationResult = timeline.PostLatencyClassificationResult
type PostLatencyReasonCode = timeline.PostLatencyReasonCode
type PostTrackingIdentity = timeline.PostTrackingIdentity
type PostDeliveryTimeline = timeline.PostDeliveryTimeline

const (
	PostDelaySourceNone               = timeline.PostDelaySourceNone
	PostDelaySourceExternalCollection = timeline.PostDelaySourceExternalCollection
	PostDelaySourceInternalDelivery   = timeline.PostDelaySourceInternalDelivery
	PostDelaySourceMixed              = timeline.PostDelaySourceMixed

	PostInternalDelayCauseNone              = timeline.PostInternalDelayCauseNone
	PostInternalDelayCauseQueueWait         = timeline.PostInternalDelayCauseQueueWait
	PostInternalDelayCauseRetryAccumulation = timeline.PostInternalDelayCauseRetryAccumulation
	PostInternalDelayCauseJobFailure        = timeline.PostInternalDelayCauseJobFailure

	PostLatencyClassificationStatusInsufficientEvidence = timeline.PostLatencyClassificationStatusInsufficientEvidence
	PostLatencyClassificationStatusWithinTarget         = timeline.PostLatencyClassificationStatusWithinTarget
	PostLatencyClassificationStatusExceeded             = timeline.PostLatencyClassificationStatusExceeded

	PostLatencyClassificationEvidenceKeyAlarmLatency      = timeline.PostLatencyClassificationEvidenceKeyAlarmLatency
	PostLatencyClassificationEvidenceKeyPublishToDetect   = timeline.PostLatencyClassificationEvidenceKeyPublishToDetect
	PostLatencyClassificationEvidenceKeyInternalLatency   = timeline.PostLatencyClassificationEvidenceKeyInternalLatency
	PostLatencyClassificationEvidenceKeyQueueWait         = timeline.PostLatencyClassificationEvidenceKeyQueueWait
	PostLatencyClassificationEvidenceKeyRetryAccumulation = timeline.PostLatencyClassificationEvidenceKeyRetryAccumulation
	PostLatencyClassificationEvidenceKeyJobFailure        = timeline.PostLatencyClassificationEvidenceKeyJobFailure

	PostLatencyReasonCodeNone                 = timeline.PostLatencyReasonCodeNone
	PostLatencyReasonCodeExternalCollection   = timeline.PostLatencyReasonCodeExternalCollection
	PostLatencyReasonCodeInternalDelivery     = timeline.PostLatencyReasonCodeInternalDelivery
	PostLatencyReasonCodeMixed                = timeline.PostLatencyReasonCodeMixed
	PostLatencyReasonCodeQueueWait            = timeline.PostLatencyReasonCodeQueueWait
	PostLatencyReasonCodeRetryAccumulation    = timeline.PostLatencyReasonCodeRetryAccumulation
	PostLatencyReasonCodeJobFailure           = timeline.PostLatencyReasonCodeJobFailure
	PostLatencyReasonCodeInsufficientEvidence = timeline.PostLatencyReasonCodeInsufficientEvidence
)

var BuildPostLatencyClassification = timeline.BuildPostLatencyClassification

const postLatencyExceededThresholdMillis = timeline.PostLatencyExceededThresholdMillis

var derivePostDeliveryTimelineMetrics = timeline.DeriveMetrics

type postDeliveryTimelineScanRow struct {
	OutboxID                   int64                           `gorm:"column:outbox_id"`
	OutboxKind                 domain.OutboxKind               `gorm:"column:outbox_kind"`
	AlarmType                  domain.AlarmType                `gorm:"column:alarm_type"`
	ChannelID                  string                          `gorm:"column:channel_id"`
	PostID                     string                          `gorm:"column:post_id"`
	ContentID                  string                          `gorm:"column:content_id"`
	ActualPublishedAt          scannableTime                   `gorm:"column:actual_published_at"`
	DetectedAt                 scannableTime                   `gorm:"column:detected_at"`
	QueueEnqueuedAt            scannableTime                   `gorm:"column:queue_enqueued_at"`
	FirstAttemptStartedAt      scannableTime                   `gorm:"column:first_attempt_started_at"`
	LastAttemptStartedAt       scannableTime                   `gorm:"column:last_attempt_started_at"`
	FirstAttemptFinishedAt     scannableTime                   `gorm:"column:first_attempt_finished_at"`
	LastAttemptFinishedAt      scannableTime                   `gorm:"column:last_attempt_finished_at"`
	AlarmSentAt                scannableTime                   `gorm:"column:alarm_sent_at"`
	FirstSuccessAt             scannableTime                   `gorm:"column:first_success_at"`
	LastSuccessAt              scannableTime                   `gorm:"column:last_success_at"`
	LastFailureAt              scannableTime                   `gorm:"column:last_failure_at"`
	NextRetryAt                scannableTime                   `gorm:"column:next_retry_at"`
	SuccessSendCount           int64                           `gorm:"column:success_send_count"`
	FailedAttemptCount         int64                           `gorm:"column:failed_attempt_count"`
	MaxAttemptOrdinal          int64                           `gorm:"column:max_attempt_ordinal"`
	AlarmLatencyMillis         *int64                          `gorm:"column:alarm_latency_millis"`
	AlarmLatencyExceeded       scannableBool                   `gorm:"column:alarm_latency_exceeded"`
	StoredClassificationStatus PostLatencyClassificationStatus `gorm:"column:latency_classification_status"`
	StoredDelaySource          PostDelaySource                 `gorm:"column:delay_source"`
	StoredInternalDelayCause   PostInternalDelayCause          `gorm:"column:internal_delay_cause"`
}

func clonePostLatencyInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
