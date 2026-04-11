package outbox

import (
	"context"
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

func (r *DeliveryTelemetryRepository) ListPostDeliveryTimelinesSince(ctx context.Context, since time.Time) ([]PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery timelines since: db is nil")
	}
	if since.IsZero() {
		return nil, fmt.Errorf("list post delivery timelines since: since is empty")
	}

	sinceUTC := since.UTC()
	rows, err := r.listPostDeliveryTimelines(ctx, &sinceUTC, nil, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines since: %w", err)
	}
	return rows, nil
}

func (r *DeliveryTelemetryRepository) ListPostDeliveryTimelinesWithinPublishedWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
) ([]PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery timelines within published window: db is nil")
	}
	if windowStart.IsZero() {
		return nil, fmt.Errorf("list post delivery timelines within published window: window start is empty")
	}
	if windowEnd.IsZero() {
		return nil, fmt.Errorf("list post delivery timelines within published window: window end is empty")
	}

	startUTC := windowStart.UTC()
	endUTC := windowEnd.UTC()
	if !startUTC.Before(endUTC) {
		return nil, fmt.Errorf("list post delivery timelines within published window: window start must be before window end")
	}

	rows, err := r.listPostDeliveryTimelines(ctx, &startUTC, &endUTC, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines within published window: %w", err)
	}
	return rows, nil
}

func (r *DeliveryTelemetryRepository) ListPostDeliveryTimelinesWithinObservationWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
	detectedBefore time.Time,
) ([]PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery timelines within observation window: db is nil")
	}
	if windowStart.IsZero() {
		return nil, fmt.Errorf("list post delivery timelines within observation window: window start is empty")
	}
	if windowEnd.IsZero() {
		return nil, fmt.Errorf("list post delivery timelines within observation window: window end is empty")
	}
	if detectedBefore.IsZero() {
		return nil, fmt.Errorf("list post delivery timelines within observation window: detected before is empty")
	}

	startUTC := windowStart.UTC()
	endUTC := windowEnd.UTC()
	detectedBeforeUTC := detectedBefore.UTC()
	if !startUTC.Before(endUTC) {
		return nil, fmt.Errorf("list post delivery timelines within observation window: window start must be before window end")
	}
	if detectedBeforeUTC.Before(endUTC) {
		return nil, fmt.Errorf("list post delivery timelines within observation window: detected before must be on or after window end")
	}

	rows, err := r.listPostDeliveryTimelines(ctx, &startUTC, &endUTC, &detectedBeforeUTC, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines within observation window: %w", err)
	}
	return rows, nil
}

func (r *DeliveryTelemetryRepository) ListPostDeliveryTimelinesByFinalizedObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
) ([]PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery timelines by finalized observation window: db is nil")
	}

	normalizedRuntimeName := strings.TrimSpace(runtimeName)
	if normalizedRuntimeName == "" {
		return nil, fmt.Errorf("list post delivery timelines by finalized observation window: runtime name is empty")
	}
	if bigBangCutoverAt.IsZero() {
		return nil, fmt.Errorf("list post delivery timelines by finalized observation window: big-bang cutover at is empty")
	}

	var scanned []postDeliveryTimelineScanRow
	query := r.db.WithContext(ctx).
		Table("youtube_community_shorts_observation_post_baselines AS base").
		Select(strings.Join([]string{
			"COALESCE(MAX(o.id), 0) AS outbox_id",
			"base.kind AS outbox_kind",
			"CASE base.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' WHEN 'NEW_SHORT' THEN 'SHORTS' ELSE 'LIVE' END AS alarm_type",
			"COALESCE(track.channel_id, base.channel_id) AS channel_id",
			"base.post_id AS post_id",
			"COALESCE(track.content_id, base.post_id) AS content_id",
			"COALESCE(track.actual_published_at, base.actual_published_at) AS actual_published_at",
			"COALESCE(track.detected_at, base.detected_at) AS detected_at",
			"MIN(o.created_at) AS queue_enqueued_at",
			"MIN(t.attempt_started_at) AS first_attempt_started_at",
			"MAX(t.attempt_started_at) AS last_attempt_started_at",
			"MIN(COALESCE(t.attempt_finished_at, t.event_at)) AS first_attempt_finished_at",
			"MAX(COALESCE(t.attempt_finished_at, t.event_at)) AS last_attempt_finished_at",
			"track.alarm_sent_at AS alarm_sent_at",
			"MIN(CASE WHEN t.send_result = 'success' THEN COALESCE(t.attempt_finished_at, t.event_at) END) AS first_success_at",
			"MAX(CASE WHEN t.send_result = 'success' THEN COALESCE(t.attempt_finished_at, t.event_at) END) AS last_success_at",
			"MAX(CASE WHEN t.send_result <> 'success' THEN COALESCE(t.attempt_finished_at, t.event_at) END) AS last_failure_at",
			"MAX(CASE WHEN t.send_result <> 'success' AND t.next_attempt_at > COALESCE(t.attempt_finished_at, t.event_at) THEN t.next_attempt_at END) AS next_retry_at",
			"COALESCE(SUM(CASE WHEN t.send_result = 'success' THEN 1 ELSE 0 END), 0) AS success_send_count",
			"COALESCE(SUM(CASE WHEN t.send_result <> 'success' THEN 1 ELSE 0 END), 0) AS failed_attempt_count",
			"COALESCE(MAX(t.attempt_ordinal), 0) AS max_attempt_ordinal",
			"track.alarm_latency_millis AS alarm_latency_millis",
			"track.alarm_latency_exceeded AS alarm_latency_exceeded",
			"track.latency_classification_status AS latency_classification_status",
			"track.delay_source AS delay_source",
			"track.internal_delay_cause AS internal_delay_cause",
		}, ", ")).
		Joins("LEFT JOIN youtube_content_alarm_tracking track ON track.kind = base.kind AND track.canonical_content_id = base.post_id").
		Joins("LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id").
		Joins("LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id").
		Where("base.runtime_name = ?", normalizedRuntimeName).
		Where("base.bigbang_cutover_at = ?", bigBangCutoverAt.UTC()).
		Group(strings.Join([]string{
			"base.kind",
			"base.channel_id",
			"base.post_id",
			"base.actual_published_at",
			"base.detected_at",
			"track.channel_id",
			"track.content_id",
			"track.actual_published_at",
			"track.detected_at",
			"track.alarm_sent_at",
			"track.alarm_latency_millis",
			"track.alarm_latency_exceeded",
			"track.latency_classification_status",
			"track.delay_source",
			"track.internal_delay_cause",
		}, ", ")).
		Order("COALESCE(track.alarm_sent_at, MAX(COALESCE(t.attempt_finished_at, t.event_at)), track.actual_published_at, base.actual_published_at, track.detected_at, base.detected_at) DESC").
		Order("base.post_id ASC")
	if err := query.Scan(&scanned).Error; err != nil {
		return nil, fmt.Errorf("list post delivery timelines by finalized observation window: scan rows: %w", err)
	}

	return buildPostDeliveryTimelinesFromScanRows(scanned), nil
}

func (r *DeliveryTelemetryRepository) ListPostDeliveryTimelinesByOutboxIDs(ctx context.Context, outboxIDs []int64) ([]PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery timelines by outbox ids: db is nil")
	}

	uniqueIDs := uniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return []PostDeliveryTimeline{}, nil
	}

	rows, err := r.listPostDeliveryTimelines(ctx, nil, nil, nil, uniqueIDs, nil)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by outbox ids: %w", err)
	}
	return rows, nil
}

func (r *DeliveryTelemetryRepository) ListPostDeliveryTimelinesByTrackingIdentities(
	ctx context.Context,
	identities []PostTrackingIdentity,
) ([]PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery timelines by tracking identities: db is nil")
	}

	normalized, err := normalizePostTrackingIdentities(identities)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by tracking identities: %w", err)
	}
	if len(normalized) == 0 {
		return []PostDeliveryTimeline{}, nil
	}

	rows, err := r.listPostDeliveryTimelines(ctx, nil, nil, nil, nil, normalized)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by tracking identities: %w", err)
	}
	return rows, nil
}

func (r *DeliveryTelemetryRepository) PersistPostLatencyClassificationsByOutboxIDs(ctx context.Context, outboxIDs []int64) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("persist post latency classifications by outbox ids: db is nil")
	}

	uniqueIDs := uniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	rows, err := r.ListPostDeliveryTimelinesByOutboxIDs(ctx, uniqueIDs)
	if err != nil {
		return fmt.Errorf("persist post latency classifications by outbox ids: %w", err)
	}
	if err := r.persistPostLatencyClassifications(ctx, rows); err != nil {
		return fmt.Errorf("persist post latency classifications by outbox ids: %w", err)
	}
	return nil
}

func (r *DeliveryTelemetryRepository) PersistPostLatencyClassificationsByIdentities(
	ctx context.Context,
	identities []PostTrackingIdentity,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("persist post latency classifications by identities: db is nil")
	}

	normalized, err := normalizePostTrackingIdentities(identities)
	if err != nil {
		return fmt.Errorf("persist post latency classifications by identities: %w", err)
	}
	if len(normalized) == 0 {
		return nil
	}

	rows, err := r.ListPostDeliveryTimelinesByTrackingIdentities(ctx, normalized)
	if err != nil {
		return fmt.Errorf("persist post latency classifications by identities: %w", err)
	}
	if err := r.persistPostLatencyClassifications(ctx, rows); err != nil {
		return fmt.Errorf("persist post latency classifications by identities: %w", err)
	}
	return nil
}

func (r *DeliveryTelemetryRepository) listPostDeliveryTimelines(
	ctx context.Context,
	windowStart *time.Time,
	windowEnd *time.Time,
	detectedBefore *time.Time,
	outboxIDs []int64,
	identities []PostTrackingIdentity,
) ([]PostDeliveryTimeline, error) {
	var scanned []postDeliveryTimelineScanRow
	query := r.db.WithContext(ctx).
		Table("youtube_content_alarm_tracking AS track").
		Select(strings.Join([]string{
			"COALESCE(MAX(o.id), 0) AS outbox_id",
			"track.kind AS outbox_kind",
			"CASE track.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' WHEN 'NEW_SHORT' THEN 'SHORTS' ELSE 'LIVE' END AS alarm_type",
			"track.channel_id AS channel_id",
			"COALESCE(MAX(NULLIF(t.post_id, '')), track.content_id) AS post_id",
			"track.content_id AS content_id",
			"track.actual_published_at AS actual_published_at",
			"track.detected_at AS detected_at",
			"MIN(o.created_at) AS queue_enqueued_at",
			"MIN(t.attempt_started_at) AS first_attempt_started_at",
			"MAX(t.attempt_started_at) AS last_attempt_started_at",
			"MIN(COALESCE(t.attempt_finished_at, t.event_at)) AS first_attempt_finished_at",
			"MAX(COALESCE(t.attempt_finished_at, t.event_at)) AS last_attempt_finished_at",
			"track.alarm_sent_at AS alarm_sent_at",
			"MIN(CASE WHEN t.send_result = 'success' THEN COALESCE(t.attempt_finished_at, t.event_at) END) AS first_success_at",
			"MAX(CASE WHEN t.send_result = 'success' THEN COALESCE(t.attempt_finished_at, t.event_at) END) AS last_success_at",
			"MAX(CASE WHEN t.send_result <> 'success' THEN COALESCE(t.attempt_finished_at, t.event_at) END) AS last_failure_at",
			"MAX(CASE WHEN t.send_result <> 'success' AND t.next_attempt_at > COALESCE(t.attempt_finished_at, t.event_at) THEN t.next_attempt_at END) AS next_retry_at",
			"COALESCE(SUM(CASE WHEN t.send_result = 'success' THEN 1 ELSE 0 END), 0) AS success_send_count",
			"COALESCE(SUM(CASE WHEN t.send_result <> 'success' THEN 1 ELSE 0 END), 0) AS failed_attempt_count",
			"COALESCE(MAX(t.attempt_ordinal), 0) AS max_attempt_ordinal",
			"track.alarm_latency_millis AS alarm_latency_millis",
			"track.alarm_latency_exceeded AS alarm_latency_exceeded",
			"track.latency_classification_status AS latency_classification_status",
			"track.delay_source AS delay_source",
			"track.internal_delay_cause AS internal_delay_cause",
		}, ", ")).
		Joins("LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id")
	if windowStart != nil {
		query = query.Joins("LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id AND t.event_at >= ?", windowStart.UTC())
	} else {
		query = query.Joins("LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id")
	}
	query = query.Where("track.kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort})
	if windowStart != nil {
		query = query.Where("COALESCE(track.actual_published_at, track.detected_at) >= ?", windowStart.UTC())
	}
	if windowEnd != nil {
		query = query.Where("COALESCE(track.actual_published_at, track.detected_at) < ?", windowEnd.UTC())
	}
	if detectedBefore != nil {
		query = query.Where("track.detected_at < ?", detectedBefore.UTC())
	}
	if len(outboxIDs) > 0 {
		query = query.Where("o.id IN ?", outboxIDs)
	}
	if len(identities) > 0 {
		clauses := make([]string, 0, len(identities))
		args := make([]any, 0, len(identities)*2)
		for i := range identities {
			clauses = append(clauses, "(track.kind = ? AND track.content_id = ?)")
			args = append(args, identities[i].Kind, identities[i].ContentID)
		}
		query = query.Where(strings.Join(clauses, " OR "), args...)
	}
	query = query.Group(strings.Join([]string{
		"track.kind",
		"track.channel_id",
		"track.content_id",
		"track.actual_published_at",
		"track.detected_at",
		"track.alarm_sent_at",
		"track.alarm_latency_millis",
		"track.alarm_latency_exceeded",
		"track.latency_classification_status",
		"track.delay_source",
		"track.internal_delay_cause",
	}, ", ")).
		Order("COALESCE(track.alarm_sent_at, MAX(COALESCE(t.attempt_finished_at, t.event_at)), track.actual_published_at, track.detected_at) DESC").
		Order("track.content_id ASC")
	if err := query.Scan(&scanned).Error; err != nil {
		return nil, fmt.Errorf("scan rows: %w", err)
	}

	return buildPostDeliveryTimelinesFromScanRows(scanned), nil
}

func buildPostDeliveryTimelinesFromScanRows(scanned []postDeliveryTimelineScanRow) []PostDeliveryTimeline {
	rows := make([]PostDeliveryTimeline, 0, len(scanned))
	for i := range scanned {
		row := PostDeliveryTimeline{
			OutboxID:                   scanned[i].OutboxID,
			OutboxKind:                 scanned[i].OutboxKind,
			AlarmType:                  scanned[i].AlarmType,
			ChannelID:                  scanned[i].ChannelID,
			PostID:                     scanned[i].PostID,
			ContentID:                  scanned[i].ContentID,
			ActualPublishedAt:          scanned[i].ActualPublishedAt.Ptr(),
			DetectedAt:                 scanned[i].DetectedAt.Ptr(),
			QueueEnqueuedAt:            scanned[i].QueueEnqueuedAt.Ptr(),
			FirstAttemptStartedAt:      scanned[i].FirstAttemptStartedAt.Ptr(),
			LastAttemptStartedAt:       scanned[i].LastAttemptStartedAt.Ptr(),
			FirstAttemptFinishedAt:     scanned[i].FirstAttemptFinishedAt.Ptr(),
			LastAttemptFinishedAt:      scanned[i].LastAttemptFinishedAt.Ptr(),
			AlarmSentAt:                scanned[i].AlarmSentAt.Ptr(),
			FirstSuccessAt:             scanned[i].FirstSuccessAt.Ptr(),
			LastSuccessAt:              scanned[i].LastSuccessAt.Ptr(),
			LastFailureAt:              scanned[i].LastFailureAt.Ptr(),
			NextRetryAt:                scanned[i].NextRetryAt.Ptr(),
			SuccessSendCount:           scanned[i].SuccessSendCount,
			FailedAttemptCount:         scanned[i].FailedAttemptCount,
			MaxAttemptOrdinal:          scanned[i].MaxAttemptOrdinal,
			AlarmLatencyMillis:         scanned[i].AlarmLatencyMillis,
			AlarmLatencyExceeded:       scanned[i].AlarmLatencyExceeded.Ptr(),
			StoredClassificationStatus: scanned[i].StoredClassificationStatus,
			StoredDelaySource:          scanned[i].StoredDelaySource,
			StoredInternalDelayCause:   scanned[i].StoredInternalDelayCause,
		}
		derivePostDeliveryTimelineMetrics(&row)
		rows = append(rows, row)
	}
	return rows
}

func derivePostDeliveryTimelineMetrics(row *PostDeliveryTimeline) {
	if row == nil {
		return
	}

	if row.MaxAttemptOrdinal > 1 {
		row.RetryAttemptCount = row.MaxAttemptOrdinal - 1
	}
	row.PublishToDetectMillis = durationMillisBetween(row.ActualPublishedAt, row.DetectedAt)
	row.DetectToQueueMillis = durationMillisBetween(row.DetectedAt, row.QueueEnqueuedAt)
	row.QueueToFirstAttemptMillis = durationMillisBetween(row.QueueEnqueuedAt, row.FirstAttemptStartedAt)
	row.FirstAttemptToFinishMillis = durationMillisBetween(row.FirstAttemptStartedAt, row.FirstAttemptFinishedAt)
	row.FirstAttemptToSuccessMillis = durationMillisBetween(row.FirstAttemptStartedAt, row.FirstSuccessAt)
	row.InternalLatencyMillis = durationMillisBetween(row.DetectedAt, row.AlarmSentAt)
	if row.InternalLatencyMillis != nil {
		row.InternalLatencyExceeded = boolPtr(*row.InternalLatencyMillis > postLatencyExceededThresholdMillis)
	}
	row.DelaySource = classifyDelaySource(row)
	row.QueueWaitMillis = sumDurationMillis(row.DetectToQueueMillis, row.QueueToFirstAttemptMillis)
	row.RetryAccumulationMillis = deriveRetryAccumulationMillis(row)
	row.JobFailureDetected = isJobFailureDetected(row)
	row.InternalDelayCause = classifyPrimaryInternalDelayCause(row)
	row.LatencyClassification = buildPostLatencyClassification(row)
}

func durationMillisBetween(start, end *time.Time) *int64 {
	if start == nil || end == nil {
		return nil
	}

	startUTC := start.UTC()
	endUTC := end.UTC()
	millis := endUTC.Sub(startUTC).Milliseconds()
	return &millis
}

func sumDurationMillis(values ...*int64) *int64 {
	var total int64
	hasValue := false
	for i := range values {
		if values[i] == nil {
			continue
		}
		total += *values[i]
		hasValue = true
	}
	if !hasValue {
		return nil
	}
	return &total
}

func deriveRetryAccumulationMillis(row *PostDeliveryTimeline) *int64 {
	if row == nil || row.FailedAttemptCount <= 0 || row.FirstAttemptFinishedAt == nil {
		return nil
	}

	endAt := resolveRetryAccumulationEnd(row)
	if endAt == nil {
		return nil
	}

	millis := durationMillisBetween(row.FirstAttemptFinishedAt, endAt)
	if millis == nil || *millis <= 0 {
		return nil
	}
	return millis
}

func resolveRetryAccumulationEnd(row *PostDeliveryTimeline) *time.Time {
	for _, candidate := range []*time.Time{row.AlarmSentAt, row.FirstSuccessAt, row.NextRetryAt, row.LastAttemptFinishedAt, row.LastFailureAt} {
		if candidate == nil || candidate.IsZero() {
			continue
		}
		if candidate.UTC().After(row.FirstAttemptFinishedAt.UTC()) {
			resolved := candidate.UTC()
			return &resolved
		}
	}
	return nil
}

func isJobFailureDetected(row *PostDeliveryTimeline) bool {
	if row == nil || row.LastFailureAt == nil {
		return false
	}
	if row.AlarmSentAt != nil || row.FirstSuccessAt != nil || row.LastSuccessAt != nil {
		return false
	}
	return true
}

func classifyDelaySource(row *PostDeliveryTimeline) PostDelaySource {
	if row == nil {
		return PostDelaySourceNone
	}

	externalMillis, hasExternal := positiveDurationMillis(row.PublishToDetectMillis)
	internalMillis, hasInternal := positiveDurationMillis(row.InternalLatencyMillis)

	if row.AlarmLatencyExceeded != nil {
		if !*row.AlarmLatencyExceeded {
			if row.InternalLatencyExceeded != nil && *row.InternalLatencyExceeded {
				return selectDominantDelaySource(externalMillis, hasExternal, internalMillis, hasInternal)
			}
			return PostDelaySourceNone
		}
		return selectDominantDelaySource(externalMillis, hasExternal, internalMillis, hasInternal)
	}

	if row.InternalLatencyExceeded != nil && *row.InternalLatencyExceeded {
		return selectDominantDelaySource(externalMillis, hasExternal, internalMillis, hasInternal)
	}
	if row.PublishToDetectMillis != nil && *row.PublishToDetectMillis > postLatencyExceededThresholdMillis {
		return selectDominantDelaySource(externalMillis, hasExternal, internalMillis, hasInternal)
	}

	return PostDelaySourceNone
}

func positiveDurationMillis(value *int64) (int64, bool) {
	if value == nil || *value <= 0 {
		return 0, false
	}
	return *value, true
}

func selectDominantDelaySource(externalMillis int64, hasExternal bool, internalMillis int64, hasInternal bool) PostDelaySource {
	switch {
	case hasExternal && !hasInternal:
		return PostDelaySourceExternalCollection
	case !hasExternal && hasInternal:
		return PostDelaySourceInternalDelivery
	case !hasExternal && !hasInternal:
		return PostDelaySourceNone
	case externalMillis > internalMillis:
		return PostDelaySourceExternalCollection
	case internalMillis > externalMillis:
		return PostDelaySourceInternalDelivery
	default:
		return PostDelaySourceMixed
	}
}

func classifyPrimaryInternalDelayCause(row *PostDeliveryTimeline) PostInternalDelayCause {
	if row == nil {
		return PostInternalDelayCauseNone
	}

	if row.JobFailureDetected {
		return PostInternalDelayCauseJobFailure
	}

	candidates := []postInternalDelayCauseCandidate{
		{
			cause:     PostInternalDelayCauseRetryAccumulation,
			millis:    row.RetryAccumulationMillis,
			priority:  postInternalDelayCausePriorityRetryAccumulation,
			available: row.RetryAccumulationMillis != nil && *row.RetryAccumulationMillis > 0,
		},
		{
			cause:     PostInternalDelayCauseQueueWait,
			millis:    row.QueueWaitMillis,
			priority:  postInternalDelayCausePriorityQueueWait,
			available: row.QueueWaitMillis != nil && *row.QueueWaitMillis > 0,
		},
	}

	selected := postInternalDelayCauseCandidate{cause: PostInternalDelayCauseNone}
	for i := range candidates {
		if !candidates[i].available {
			continue
		}
		if selected.cause == PostInternalDelayCauseNone {
			selected = candidates[i]
			continue
		}
		if *candidates[i].millis > *selected.millis {
			selected = candidates[i]
			continue
		}
		if *candidates[i].millis == *selected.millis && candidates[i].priority > selected.priority {
			selected = candidates[i]
		}
	}

	return selected.cause
}

func BuildPostLatencyClassification(row PostDeliveryTimeline) PostLatencyClassificationResult {
	return buildPostLatencyClassification(&row)
}

func buildPostLatencyClassification(row *PostDeliveryTimeline) PostLatencyClassificationResult {
	delaySource := PostDelaySourceNone
	internalDelayCause := PostInternalDelayCauseNone
	if row != nil {
		if row.DelaySource != "" {
			delaySource = row.DelaySource
		}
		if row.InternalDelayCause != "" {
			internalDelayCause = row.InternalDelayCause
		}
	}

	return PostLatencyClassificationResult{
		Status:             classifyPostLatencyClassificationStatus(row),
		ThresholdMillis:    postLatencyExceededThresholdMillis,
		DelaySource:        delaySource,
		InternalDelayCause: internalDelayCause,
		Evidence:           buildPostLatencyClassificationEvidence(row),
	}
}

func classifyPostLatencyReasonCode(classification PostLatencyClassificationResult) PostLatencyReasonCode {
	switch classification.DelaySource {
	case PostDelaySourceExternalCollection:
		return PostLatencyReasonCodeExternalCollection
	case PostDelaySourceMixed:
		return PostLatencyReasonCodeMixed
	}

	switch classification.InternalDelayCause {
	case PostInternalDelayCauseQueueWait:
		return PostLatencyReasonCodeQueueWait
	case PostInternalDelayCauseRetryAccumulation:
		return PostLatencyReasonCodeRetryAccumulation
	case PostInternalDelayCauseJobFailure:
		return PostLatencyReasonCodeJobFailure
	}

	if classification.DelaySource == PostDelaySourceInternalDelivery {
		return PostLatencyReasonCodeInternalDelivery
	}
	if classification.Status == PostLatencyClassificationStatusInsufficientEvidence {
		return PostLatencyReasonCodeInsufficientEvidence
	}
	return PostLatencyReasonCodeNone
}

func classifyPostLatencyClassificationStatus(row *PostDeliveryTimeline) PostLatencyClassificationStatus {
	if row == nil {
		return PostLatencyClassificationStatusInsufficientEvidence
	}
	if row.AlarmLatencyExceeded != nil {
		if *row.AlarmLatencyExceeded {
			return PostLatencyClassificationStatusExceeded
		}
		return PostLatencyClassificationStatusWithinTarget
	}
	if row.InternalLatencyExceeded != nil && *row.InternalLatencyExceeded {
		return PostLatencyClassificationStatusExceeded
	}
	if row.PublishToDetectMillis != nil && *row.PublishToDetectMillis > postLatencyExceededThresholdMillis {
		return PostLatencyClassificationStatusExceeded
	}
	if row.QueueWaitMillis != nil && *row.QueueWaitMillis > postLatencyExceededThresholdMillis {
		return PostLatencyClassificationStatusExceeded
	}
	if row.RetryAccumulationMillis != nil && *row.RetryAccumulationMillis > postLatencyExceededThresholdMillis {
		return PostLatencyClassificationStatusExceeded
	}
	return PostLatencyClassificationStatusInsufficientEvidence
}

func buildPostLatencyClassificationEvidence(row *PostDeliveryTimeline) []PostLatencyClassificationEvidence {
	if row == nil {
		return []PostLatencyClassificationEvidence{}
	}

	selectExternal := row.DelaySource == PostDelaySourceExternalCollection || row.DelaySource == PostDelaySourceMixed
	selectInternal := row.DelaySource == PostDelaySourceInternalDelivery || row.DelaySource == PostDelaySourceMixed
	if row.InternalLatencyExceeded != nil && *row.InternalLatencyExceeded {
		selectInternal = true
	}

	return []PostLatencyClassificationEvidence{
		{
			Key:      PostLatencyClassificationEvidenceKeyAlarmLatency,
			Millis:   clonePostLatencyInt64(row.AlarmLatencyMillis),
			Selected: row.AlarmLatencyExceeded != nil,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyPublishToDetect,
			Millis:   clonePostLatencyInt64(row.PublishToDetectMillis),
			Selected: selectExternal,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyInternalLatency,
			Millis:   clonePostLatencyInt64(row.InternalLatencyMillis),
			Selected: selectInternal,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyQueueWait,
			Millis:   clonePostLatencyInt64(row.QueueWaitMillis),
			Selected: row.InternalDelayCause == PostInternalDelayCauseQueueWait,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyRetryAccumulation,
			Millis:   clonePostLatencyInt64(row.RetryAccumulationMillis),
			Selected: row.InternalDelayCause == PostInternalDelayCauseRetryAccumulation,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyJobFailure,
			Bool:     boolPtr(row.JobFailureDetected),
			Selected: row.InternalDelayCause == PostInternalDelayCauseJobFailure,
		},
	}
}

func clonePostLatencyInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func normalizePostTrackingIdentities(identities []PostTrackingIdentity) ([]PostTrackingIdentity, error) {
	if len(identities) == 0 {
		return nil, nil
	}

	normalized := make([]PostTrackingIdentity, 0, len(identities))
	seen := make(map[string]struct{}, len(identities))
	for i := range identities {
		contentID := strings.TrimSpace(identities[i].ContentID)
		if contentID == "" {
			continue
		}
		switch identities[i].Kind {
		case domain.OutboxKindCommunityPost, domain.OutboxKindNewShort:
		default:
			return nil, fmt.Errorf("unsupported tracking identity kind: %s", identities[i].Kind)
		}
		key := postTrackingIdentityKey(identities[i].Kind, contentID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, PostTrackingIdentity{Kind: identities[i].Kind, ContentID: contentID})
	}
	return normalized, nil
}

func postTrackingIdentityKey(kind domain.OutboxKind, contentID string) string {
	trimmed := strings.TrimSpace(contentID)
	if trimmed == "" {
		return ""
	}
	return string(kind) + ":" + trimmed
}

func (r *DeliveryTelemetryRepository) persistPostLatencyClassifications(ctx context.Context, rows []PostDeliveryTimeline) error {
	if len(rows) == 0 {
		return nil
	}

	updatedAt := time.Now().UTC()
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		if !isCommunityShortsDeliveryAuditKind(rows[i].OutboxKind) {
			continue
		}
		contentID := strings.TrimSpace(rows[i].ContentID)
		if contentID == "" {
			continue
		}
		key := postTrackingIdentityKey(rows[i].OutboxKind, contentID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		status := rows[i].LatencyClassification.Status
		if status == "" {
			status = PostLatencyClassificationStatusInsufficientEvidence
		}
		delaySource := rows[i].DelaySource
		if delaySource == "" {
			delaySource = PostDelaySourceNone
		}
		internalDelayCause := rows[i].InternalDelayCause
		if internalDelayCause == "" {
			internalDelayCause = PostInternalDelayCauseNone
		}

		if err := r.db.WithContext(ctx).
			Model(&domain.YouTubeContentAlarmTracking{}).
			Where("kind = ? AND content_id = ?", rows[i].OutboxKind, contentID).
			Updates(map[string]any{
				"latency_classification_status": string(status),
				"delay_source":                  string(delaySource),
				"internal_delay_cause":          string(internalDelayCause),
				"updated_at":                    updatedAt,
			}).Error; err != nil {
			return fmt.Errorf("update persisted latency classification: kind=%s content_id=%s: %w", rows[i].OutboxKind, contentID, err)
		}
	}

	return nil
}
