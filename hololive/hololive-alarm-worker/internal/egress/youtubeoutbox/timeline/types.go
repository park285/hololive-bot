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
	OutboxID                    int64                           `db:"outbox_id"`
	OutboxKind                  domain.OutboxKind               `db:"outbox_kind"`
	AlarmType                   domain.AlarmType                `db:"alarm_type"`
	ChannelID                   string                          `db:"channel_id"`
	PostID                      string                          `db:"post_id"`
	ContentID                   string                          `db:"content_id"`
	ActualPublishedAt           *time.Time                      `db:"actual_published_at"`
	DetectedAt                  *time.Time                      `db:"detected_at"`
	QueueEnqueuedAt             *time.Time                      `db:"queue_enqueued_at"`
	FirstAttemptStartedAt       *time.Time                      `db:"first_attempt_started_at"`
	LastAttemptStartedAt        *time.Time                      `db:"last_attempt_started_at"`
	FirstAttemptFinishedAt      *time.Time                      `db:"first_attempt_finished_at"`
	LastAttemptFinishedAt       *time.Time                      `db:"last_attempt_finished_at"`
	AlarmSentAt                 *time.Time                      `db:"alarm_sent_at"`
	FirstSuccessAt              *time.Time                      `db:"first_success_at"`
	LastSuccessAt               *time.Time                      `db:"last_success_at"`
	LastFailureAt               *time.Time                      `db:"last_failure_at"`
	NextRetryAt                 *time.Time                      `db:"next_retry_at"`
	SuccessSendCount            int64                           `db:"success_send_count"`
	FailedAttemptCount          int64                           `db:"failed_attempt_count"`
	MaxAttemptOrdinal           int64                           `db:"max_attempt_ordinal"`
	RetryAttemptCount           int64                           `db:"-"`
	AlarmLatencyMillis          *int64                          `db:"alarm_latency_millis"`
	AlarmLatencyExceeded        *bool                           `db:"-"`
	StoredClassificationStatus  PostLatencyClassificationStatus `db:"latency_classification_status"`
	StoredDelaySource           PostDelaySource                 `db:"delay_source"`
	StoredInternalDelayCause    PostInternalDelayCause          `db:"internal_delay_cause"`
	PublishToDetectMillis       *int64                          `db:"-"`
	DetectToQueueMillis         *int64                          `db:"-"`
	QueueToFirstAttemptMillis   *int64                          `db:"-"`
	FirstAttemptToFinishMillis  *int64                          `db:"-"`
	FirstAttemptToSuccessMillis *int64                          `db:"-"`
	InternalLatencyMillis       *int64                          `db:"-"`
	InternalLatencyExceeded     *bool                           `db:"-"`
	DelaySource                 PostDelaySource                 `db:"-"`
	QueueWaitMillis             *int64                          `db:"-"`
	RetryAccumulationMillis     *int64                          `db:"-"`
	JobFailureDetected          bool                            `db:"-"`
	InternalDelayCause          PostInternalDelayCause          `db:"-"`
	LatencyClassification       PostLatencyClassificationResult `db:"-"`
}
