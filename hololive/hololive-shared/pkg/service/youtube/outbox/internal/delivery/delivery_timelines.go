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
	OutboxID                   int64                           `db:"outbox_id"`
	OutboxKind                 domain.OutboxKind               `db:"outbox_kind"`
	AlarmType                  domain.AlarmType                `db:"alarm_type"`
	ChannelID                  string                          `db:"channel_id"`
	PostID                     string                          `db:"post_id"`
	ContentID                  string                          `db:"content_id"`
	ActualPublishedAt          scannableTime                   `db:"actual_published_at"`
	DetectedAt                 scannableTime                   `db:"detected_at"`
	QueueEnqueuedAt            scannableTime                   `db:"queue_enqueued_at"`
	FirstAttemptStartedAt      scannableTime                   `db:"first_attempt_started_at"`
	LastAttemptStartedAt       scannableTime                   `db:"last_attempt_started_at"`
	FirstAttemptFinishedAt     scannableTime                   `db:"first_attempt_finished_at"`
	LastAttemptFinishedAt      scannableTime                   `db:"last_attempt_finished_at"`
	AlarmSentAt                scannableTime                   `db:"alarm_sent_at"`
	FirstSuccessAt             scannableTime                   `db:"first_success_at"`
	LastSuccessAt              scannableTime                   `db:"last_success_at"`
	LastFailureAt              scannableTime                   `db:"last_failure_at"`
	NextRetryAt                scannableTime                   `db:"next_retry_at"`
	SuccessSendCount           int64                           `db:"success_send_count"`
	FailedAttemptCount         int64                           `db:"failed_attempt_count"`
	MaxAttemptOrdinal          int64                           `db:"max_attempt_ordinal"`
	AlarmLatencyMillis         *int64                          `db:"alarm_latency_millis"`
	AlarmLatencyExceeded       scannableBool                   `db:"alarm_latency_exceeded"`
	StoredClassificationStatus PostLatencyClassificationStatus `db:"latency_classification_status"`
	StoredDelaySource          PostDelaySource                 `db:"delay_source"`
	StoredInternalDelayCause   PostInternalDelayCause          `db:"internal_delay_cause"`
}

func clonePostLatencyInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
