package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

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
	if err := selectDeliverySQL(ctx, r.db, &scanned, "list post delivery timelines by finalized observation window: scan rows", `
		SELECT `+finalizedObservationTimelineSelect()+`
		FROM youtube_community_shorts_observation_post_baselines AS base
		LEFT JOIN youtube_content_alarm_tracking track ON track.kind = base.kind AND track.canonical_content_id = base.post_id
		LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id
		LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id
		WHERE base.runtime_name = ?
		  AND base.bigbang_cutover_at = ?
		GROUP BY `+finalizedObservationTimelineGroup()+`
		ORDER BY COALESCE(track.alarm_sent_at, MAX(COALESCE(t.attempt_finished_at, t.event_at)), track.actual_published_at, base.actual_published_at, track.detected_at, base.detected_at) DESC,
		         base.post_id ASC
	`, normalizedRuntimeName, bigBangCutoverAt.UTC()); err != nil {
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

	normalized, err := timeline.NormalizePostTrackingIdentities(identities)
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

func (r *DeliveryTelemetryRepository) listPostDeliveryTimelines(
	ctx context.Context,
	windowStart *time.Time,
	windowEnd *time.Time,
	detectedBefore *time.Time,
	outboxIDs []int64,
	identities []PostTrackingIdentity,
) ([]PostDeliveryTimeline, error) {
	var scanned []postDeliveryTimelineScanRow
	postKinds := []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}
	query := `
		SELECT ` + postDeliveryTimelineSelect() + `
		FROM youtube_content_alarm_tracking AS track
		LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id
	`
	args := make([]any, 0)
	if windowStart != nil {
		query += " LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id AND t.event_at >= ?"
		args = append(args, windowStart.UTC())
	} else {
		query += " LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id"
	}
	query += " WHERE " + deliveryInClause("track.kind", len(postKinds))
	args = appendDeliveryOutboxKindArgs(args, postKinds...)
	if windowStart != nil {
		query += " AND COALESCE(track.actual_published_at, track.detected_at) >= ?"
		args = append(args, windowStart.UTC())
	}
	if windowEnd != nil {
		query += " AND COALESCE(track.actual_published_at, track.detected_at) < ?"
		args = append(args, windowEnd.UTC())
	}
	if detectedBefore != nil {
		query += " AND track.detected_at < ?"
		args = append(args, detectedBefore.UTC())
	}
	if len(outboxIDs) > 0 {
		query += " AND " + deliveryInClause("o.id", len(outboxIDs))
		args = appendDeliveryInt64Args(args, outboxIDs)
	}
	if len(identities) > 0 {
		clause, identityArgs := postTrackingIdentityWhere(identities)
		query += " AND (" + clause + ")"
		args = append(args, identityArgs...)
	}
	query += `
		GROUP BY ` + postDeliveryTimelineGroup() + `
		ORDER BY COALESCE(track.alarm_sent_at, MAX(COALESCE(t.attempt_finished_at, t.event_at)), track.actual_published_at, track.detected_at) DESC,
		         track.content_id ASC
	`
	if err := selectDeliverySQL(ctx, r.db, &scanned, "scan rows", query, args...); err != nil {
		return nil, fmt.Errorf("scan rows: %w", err)
	}

	return buildPostDeliveryTimelinesFromScanRows(scanned), nil
}

func finalizedObservationTimelineSelect() string {
	return strings.Join([]string{
		"COALESCE(MAX(o.id), 0) AS outbox_id",
		"base.kind AS outbox_kind",
		"CASE base.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' WHEN 'NEW_SHORT' THEN 'SHORTS' ELSE 'LIVE' END AS alarm_type",
		"COALESCE(track.channel_id, base.channel_id) AS channel_id",
		"base.post_id AS post_id",
		"COALESCE(track.content_id, base.post_id) AS content_id",
		"COALESCE(track.actual_published_at, base.actual_published_at) AS actual_published_at",
		"COALESCE(track.detected_at, base.detected_at) AS detected_at",
	}, ", ") + ", " + postDeliveryTimelineAttemptSelect()
}

func postDeliveryTimelineSelect() string {
	return strings.Join([]string{
		"COALESCE(MAX(o.id), 0) AS outbox_id",
		"track.kind AS outbox_kind",
		"CASE track.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' WHEN 'NEW_SHORT' THEN 'SHORTS' ELSE 'LIVE' END AS alarm_type",
		"track.channel_id AS channel_id",
		"COALESCE(MAX(NULLIF(t.post_id, '')), track.content_id) AS post_id",
		"track.content_id AS content_id",
		"track.actual_published_at AS actual_published_at",
		"track.detected_at AS detected_at",
	}, ", ") + ", " + postDeliveryTimelineAttemptSelect()
}

func postDeliveryTimelineAttemptSelect() string {
	return strings.Join([]string{
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
		"COALESCE(track.latency_classification_status, '') AS latency_classification_status",
		"COALESCE(track.delay_source, '') AS delay_source",
		"COALESCE(track.internal_delay_cause, '') AS internal_delay_cause",
	}, ", ")
}

func finalizedObservationTimelineGroup() string {
	return strings.Join(append([]string{
		"base.kind",
		"base.channel_id",
		"base.post_id",
		"base.actual_published_at",
		"base.detected_at",
		"track.channel_id",
	}, postDeliveryTimelineTrackGroupColumns()...), ", ")
}

func postDeliveryTimelineGroup() string {
	return strings.Join(postDeliveryTimelineTrackGroupColumns(), ", ")
}

func postDeliveryTimelineTrackGroupColumns() []string {
	return []string{
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
	}
}

func postTrackingIdentityWhere(identities []PostTrackingIdentity) (string, []any) {
	clauses := make([]string, 0, len(identities))
	args := make([]any, 0, len(identities)*2)
	for i := range identities {
		clauses = append(clauses, "(track.kind = ? AND track.content_id = ?)")
		args = append(args, identities[i].Kind, identities[i].ContentID)
	}
	return strings.Join(clauses, " OR "), args
}
