package timeline

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const PostLatencyExceededThresholdMillis = int64((2 * time.Minute) / time.Millisecond)

type PostDelaySource string

const (
	PostDelaySourceNone               PostDelaySource = "none"
	PostDelaySourceExternalCollection PostDelaySource = "external_collection"
	PostDelaySourceInternalDelivery   PostDelaySource = "internal_delivery"
	PostDelaySourceMixed              PostDelaySource = "mixed"
)

type PostInternalDelayCause string

const (
	PostInternalDelayCauseNone              PostInternalDelayCause = "none"
	PostInternalDelayCauseQueueWait         PostInternalDelayCause = "queue_wait"
	PostInternalDelayCauseRetryAccumulation PostInternalDelayCause = "retry_accumulation"
	PostInternalDelayCauseJobFailure        PostInternalDelayCause = "job_failure"
)

const (
	PostInternalDelayCausePriorityQueueWait         = 1
	PostInternalDelayCausePriorityRetryAccumulation = 2
)

type PostInternalDelayCauseCandidate struct {
	Cause     PostInternalDelayCause
	Millis    *int64
	Priority  int
	Available bool
}

type PostLatencyClassificationStatus string

const (
	PostLatencyClassificationStatusInsufficientEvidence PostLatencyClassificationStatus = "insufficient_evidence"
	PostLatencyClassificationStatusWithinTarget         PostLatencyClassificationStatus = "within_target"
	PostLatencyClassificationStatusExceeded             PostLatencyClassificationStatus = "exceeded"
)

type PostLatencyClassificationEvidenceKey string

const (
	PostLatencyClassificationEvidenceKeyAlarmLatency      PostLatencyClassificationEvidenceKey = "alarm_latency"
	PostLatencyClassificationEvidenceKeyPublishToDetect   PostLatencyClassificationEvidenceKey = "publish_to_detect"
	PostLatencyClassificationEvidenceKeyInternalLatency   PostLatencyClassificationEvidenceKey = "internal_latency"
	PostLatencyClassificationEvidenceKeyQueueWait         PostLatencyClassificationEvidenceKey = "queue_wait"
	PostLatencyClassificationEvidenceKeyRetryAccumulation PostLatencyClassificationEvidenceKey = "retry_accumulation"
	PostLatencyClassificationEvidenceKeyJobFailure        PostLatencyClassificationEvidenceKey = "job_failure_detected"
)

type PostLatencyClassificationEvidence struct {
	Key      PostLatencyClassificationEvidenceKey `json:"key"`
	Millis   *int64                               `json:"millis,omitempty"`
	Bool     *bool                                `json:"bool,omitempty"`
	Selected bool                                 `json:"selected"`
}

type PostLatencyClassificationResult struct {
	Status             PostLatencyClassificationStatus     `json:"status"`
	ThresholdMillis    int64                               `json:"threshold_millis"`
	DelaySource        PostDelaySource                     `json:"delay_source"`
	InternalDelayCause PostInternalDelayCause              `json:"internal_delay_cause"`
	Evidence           []PostLatencyClassificationEvidence `json:"evidence"`
}

type PostLatencyReasonCode string

const (
	PostLatencyReasonCodeNone                 PostLatencyReasonCode = "none"
	PostLatencyReasonCodeExternalCollection   PostLatencyReasonCode = "external_collection"
	PostLatencyReasonCodeInternalDelivery     PostLatencyReasonCode = "internal_delivery"
	PostLatencyReasonCodeMixed                PostLatencyReasonCode = "mixed"
	PostLatencyReasonCodeQueueWait            PostLatencyReasonCode = "queue_wait"
	PostLatencyReasonCodeRetryAccumulation    PostLatencyReasonCode = "retry_accumulation"
	PostLatencyReasonCodeJobFailure           PostLatencyReasonCode = "job_failure"
	PostLatencyReasonCodeInsufficientEvidence PostLatencyReasonCode = "insufficient_evidence"
)

type PostTrackingIdentity struct {
	Kind      domain.OutboxKind
	ContentID string
}

type PostDeliveryTimeline struct {
	OutboxID                    int64                           `gorm:"column:outbox_id"`
	OutboxKind                  domain.OutboxKind               `gorm:"column:outbox_kind"`
	AlarmType                   domain.AlarmType                `gorm:"column:alarm_type"`
	ChannelID                   string                          `gorm:"column:channel_id"`
	PostID                      string                          `gorm:"column:post_id"`
	ContentID                   string                          `gorm:"column:content_id"`
	ActualPublishedAt           *time.Time                      `gorm:"column:actual_published_at"`
	DetectedAt                  *time.Time                      `gorm:"column:detected_at"`
	QueueEnqueuedAt             *time.Time                      `gorm:"column:queue_enqueued_at"`
	FirstAttemptStartedAt       *time.Time                      `gorm:"column:first_attempt_started_at"`
	LastAttemptStartedAt        *time.Time                      `gorm:"column:last_attempt_started_at"`
	FirstAttemptFinishedAt      *time.Time                      `gorm:"column:first_attempt_finished_at"`
	LastAttemptFinishedAt       *time.Time                      `gorm:"column:last_attempt_finished_at"`
	AlarmSentAt                 *time.Time                      `gorm:"column:alarm_sent_at"`
	FirstSuccessAt              *time.Time                      `gorm:"column:first_success_at"`
	LastSuccessAt               *time.Time                      `gorm:"column:last_success_at"`
	LastFailureAt               *time.Time                      `gorm:"column:last_failure_at"`
	NextRetryAt                 *time.Time                      `gorm:"column:next_retry_at"`
	SuccessSendCount            int64                           `gorm:"column:success_send_count"`
	FailedAttemptCount          int64                           `gorm:"column:failed_attempt_count"`
	MaxAttemptOrdinal           int64                           `gorm:"column:max_attempt_ordinal"`
	RetryAttemptCount           int64                           `gorm:"-"`
	AlarmLatencyMillis          *int64                          `gorm:"column:alarm_latency_millis"`
	AlarmLatencyExceeded        *bool                           `gorm:"-"`
	StoredClassificationStatus  PostLatencyClassificationStatus `gorm:"column:latency_classification_status"`
	StoredDelaySource           PostDelaySource                 `gorm:"column:delay_source"`
	StoredInternalDelayCause    PostInternalDelayCause          `gorm:"column:internal_delay_cause"`
	PublishToDetectMillis       *int64                          `gorm:"-"`
	DetectToQueueMillis         *int64                          `gorm:"-"`
	QueueToFirstAttemptMillis   *int64                          `gorm:"-"`
	FirstAttemptToFinishMillis  *int64                          `gorm:"-"`
	FirstAttemptToSuccessMillis *int64                          `gorm:"-"`
	InternalLatencyMillis       *int64                          `gorm:"-"`
	InternalLatencyExceeded     *bool                           `gorm:"-"`
	DelaySource                 PostDelaySource                 `gorm:"-"`
	QueueWaitMillis             *int64                          `gorm:"-"`
	RetryAccumulationMillis     *int64                          `gorm:"-"`
	JobFailureDetected          bool                            `gorm:"-"`
	InternalDelayCause          PostInternalDelayCause          `gorm:"-"`
	LatencyClassification       PostLatencyClassificationResult `gorm:"-"`
}
