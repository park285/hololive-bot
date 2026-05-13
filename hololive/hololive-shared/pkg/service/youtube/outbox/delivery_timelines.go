package outbox

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const postLatencyExceededThresholdMillis = int64((2 * time.Minute) / time.Millisecond)

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
	postInternalDelayCausePriorityQueueWait         = 1
	postInternalDelayCausePriorityRetryAccumulation = 2
)

type postInternalDelayCauseCandidate struct {
	cause     PostInternalDelayCause
	millis    *int64
	priority  int
	available bool
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

func normalizePostTrackingIdentities(identities []PostTrackingIdentity) ([]PostTrackingIdentity, error) {
	if len(identities) == 0 {
		return nil, nil
	}

	normalized := make([]PostTrackingIdentity, 0, len(identities))
	seen := make(map[string]struct{}, len(identities))
	for i := range identities {
		identity, ok, err := normalizePostTrackingIdentity(identities[i])
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		key := postTrackingIdentityKey(identity.Kind, identity.ContentID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, identity)
	}
	return normalized, nil
}

func normalizePostTrackingIdentity(identity PostTrackingIdentity) (PostTrackingIdentity, bool, error) {
	contentID := strings.TrimSpace(identity.ContentID)
	if contentID == "" {
		return PostTrackingIdentity{}, false, nil
	}
	switch identity.Kind {
	case domain.OutboxKindCommunityPost, domain.OutboxKindNewShort:
		return PostTrackingIdentity{Kind: identity.Kind, ContentID: contentID}, true, nil
	default:
		return PostTrackingIdentity{}, false, fmt.Errorf("unsupported tracking identity kind: %s", identity.Kind)
	}
}

func postTrackingIdentityKey(kind domain.OutboxKind, contentID string) string {
	trimmed := strings.TrimSpace(contentID)
	if trimmed == "" {
		return ""
	}
	return string(kind) + ":" + trimmed
}
