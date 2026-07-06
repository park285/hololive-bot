package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

func (r *Repository) ListPostDeliveryTimelinesSince(ctx context.Context, since time.Time) ([]PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery timelines since: db is nil")
	}
	if since.IsZero() {
		return nil, fmt.Errorf("list post delivery timelines since: since is empty")
	}

	sinceUTC := since.UTC()
	rows, err := r.listPostDeliveryTimelines(ctx, &sinceUTC, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines since: %w", err)
	}
	return rows, nil
}

func (r *Repository) ListPostDeliveryTimelinesWithinPublishedWindow(
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

	rows, err := r.listPostDeliveryTimelines(ctx, &startUTC, &endUTC, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines within published window: %w", err)
	}
	return rows, nil
}

func (r *Repository) ListPostDeliveryTimelinesByOutboxIDs(ctx context.Context, outboxIDs []int64) ([]PostDeliveryTimeline, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post delivery timelines by outbox ids: db is nil")
	}

	uniqueIDs := deliverysql.UniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return []PostDeliveryTimeline{}, nil
	}

	rows, err := r.listPostDeliveryTimelines(ctx, nil, nil, uniqueIDs, nil)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by outbox ids: %w", err)
	}
	return rows, nil
}

func (r *Repository) ListPostDeliveryTimelinesByTrackingIdentities(
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

	rows, err := r.listPostDeliveryTimelines(ctx, nil, nil, nil, normalized)
	if err != nil {
		return nil, fmt.Errorf("list post delivery timelines by tracking identities: %w", err)
	}
	return rows, nil
}

func (r *Repository) listPostDeliveryTimelines(
	ctx context.Context,
	windowStart *time.Time,
	windowEnd *time.Time,
	outboxIDs []int64,
	identities []PostTrackingIdentity,
) ([]PostDeliveryTimeline, error) {
	var scanned []postDeliveryTimelineScanRow
	postKinds := []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}
	query := mustSQL("timelines_query_0107_01.sql") + postDeliveryTimelineSelect() + `
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
	query += " WHERE " + deliverysql.DeliveryInClause("track.kind", len(postKinds))
	args = deliverysql.AppendDeliveryOutboxKindArgs(args, postKinds...)
	if windowStart != nil {
		query += " AND COALESCE(track.actual_published_at, track.detected_at) >= ?"
		args = append(args, windowStart.UTC())
	}
	if windowEnd != nil {
		query += " AND COALESCE(track.actual_published_at, track.detected_at) < ?"
		args = append(args, windowEnd.UTC())
	}
	if len(outboxIDs) > 0 {
		query += " AND " + deliverysql.DeliveryInClause("o.id", len(outboxIDs))
		args = deliverysql.AppendDeliveryInt64Args(args, outboxIDs)
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
	if err := deliverysql.SelectDeliverySQL(ctx, r.db, &scanned, "scan rows", query, args...); err != nil {
		return nil, fmt.Errorf("scan rows: %w", err)
	}

	return buildPostDeliveryTimelinesFromScanRows(scanned), nil
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

func postTrackingIdentityWhere(identities []PostTrackingIdentity) (result1 string, result2 []any) {
	clauses := make([]string, 0, len(identities))
	args := make([]any, 0, len(identities)*2)
	for i := range identities {
		clauses = append(clauses, "(track.kind = ? AND track.content_id = ?)")
		args = append(args, identities[i].Kind, identities[i].ContentID)
	}
	return strings.Join(clauses, " OR "), args
}
