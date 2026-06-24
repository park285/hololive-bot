package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/analytics"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

type PostSendCount = analytics.PostSendCount

type postSendCountScanRow struct {
	OutboxKind            domain.OutboxKind `db:"outbox_kind"`
	AlarmType             domain.AlarmType  `db:"alarm_type"`
	ChannelID             string            `db:"channel_id"`
	PostID                string            `db:"post_id"`
	ContentID             string            `db:"content_id"`
	ActualPublishedAt     scannableTime     `db:"actual_published_at"`
	DetectedAt            scannableTime     `db:"detected_at"`
	AlarmSentAt           scannableTime     `db:"alarm_sent_at"`
	AlarmLatencyMillis    *int64            `db:"alarm_latency_millis"`
	AlarmLatencyExceeded  scannableBool     `db:"alarm_latency_exceeded"`
	FirstEventAt          scannableTime     `db:"first_event_at"`
	LastEventAt           scannableTime     `db:"last_event_at"`
	FirstSuccessAt        scannableTime     `db:"first_success_at"`
	LastSuccessAt         scannableTime     `db:"last_success_at"`
	OutboxCount           int64             `db:"outbox_count"`
	SuccessSendCount      int64             `db:"success_send_count"`
	SuccessRoomCount      int64             `db:"success_room_count"`
	DuplicateSuccessCount int64             `db:"duplicate_success_count"`
	FailedAttemptCount    int64             `db:"failed_attempt_count"`
}

func (r *Repository) ListPostSendCountsSince(ctx context.Context, since time.Time) ([]PostSendCount, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post send counts since: db is nil")
	}
	if since.IsZero() {
		return nil, fmt.Errorf("list post send counts since: since is empty")
	}

	rows, err := r.listPostSendCounts(ctx, since.UTC(), nil)
	if err != nil {
		return nil, fmt.Errorf("list post send counts since: %w", err)
	}
	return rows, nil
}

func (r *Repository) ListPostSendCountsWithinPublishedWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
) ([]PostSendCount, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post send counts within published window: db is nil")
	}
	if windowStart.IsZero() {
		return nil, fmt.Errorf("list post send counts within published window: window start is empty")
	}
	if windowEnd.IsZero() {
		return nil, fmt.Errorf("list post send counts within published window: window end is empty")
	}

	startUTC := windowStart.UTC()
	endUTC := windowEnd.UTC()
	if !startUTC.Before(endUTC) {
		return nil, fmt.Errorf("list post send counts within published window: window start must be before window end")
	}

	rows, err := r.listPostSendCounts(ctx, startUTC, &endUTC)
	if err != nil {
		return nil, fmt.Errorf("list post send counts within published window: %w", err)
	}
	return rows, nil
}

func (r *Repository) listPostSendCounts(
	ctx context.Context,
	windowStart time.Time,
	windowEnd *time.Time,
) ([]PostSendCount, error) {
	var scanned []postSendCountScanRow
	postKinds := []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}
	query := `
		SELECT ` + postSendCountsSelectSQL() + `
		FROM youtube_content_alarm_tracking AS track
		LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id
		LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id AND t.event_at >= ?
		WHERE ` + deliverysql.DeliveryInClause("track.kind", len(postKinds)) + `
		  AND COALESCE(track.actual_published_at, track.detected_at) >= ?
	`
	args := []any{windowStart.UTC()}
	args = deliverysql.AppendDeliveryOutboxKindArgs(args, postKinds...)
	args = append(args, windowStart.UTC())
	if windowEnd != nil {
		query += " AND COALESCE(track.actual_published_at, track.detected_at) < ?"
		args = append(args, windowEnd.UTC())
	}
	query += postSendCountsGroupOrderSQL()
	if err := deliverysql.SelectDeliverySQL(ctx, r.db, &scanned, "scan rows", query, args...); err != nil {
		return nil, fmt.Errorf("scan rows: %w", err)
	}

	return buildPostSendCountsFromScanRows(scanned), nil
}

func postSendCountsSelectSQL() string {
	return strings.Join([]string{
		"track.kind AS outbox_kind",
		"CASE track.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' WHEN 'NEW_SHORT' THEN 'SHORTS' ELSE 'LIVE' END AS alarm_type",
		"track.channel_id AS channel_id",
		"COALESCE(MAX(NULLIF(t.post_id, '')), track.content_id) AS post_id",
		"track.content_id AS content_id",
		"track.actual_published_at AS actual_published_at",
		"track.detected_at AS detected_at",
		"track.alarm_sent_at AS alarm_sent_at",
		"track.alarm_latency_millis AS alarm_latency_millis",
		"track.alarm_latency_exceeded AS alarm_latency_exceeded",
		"MIN(t.event_at) AS first_event_at",
		"MAX(t.event_at) AS last_event_at",
		"MIN(CASE WHEN t.send_result = 'success' THEN t.event_at END) AS first_success_at",
		"MAX(CASE WHEN t.send_result = 'success' THEN t.event_at END) AS last_success_at",
		"COUNT(DISTINCT o.id) AS outbox_count",
		"COALESCE(SUM(CASE WHEN t.send_result = 'success' THEN 1 ELSE 0 END), 0) AS success_send_count",
		"COUNT(DISTINCT CASE WHEN t.send_result = 'success' THEN t.room_id END) AS success_room_count",
		"COALESCE(SUM(CASE WHEN t.send_result = 'success' THEN 1 ELSE 0 END), 0) - COUNT(DISTINCT CASE WHEN t.send_result = 'success' THEN t.room_id END) AS duplicate_success_count",
		"COALESCE(SUM(CASE WHEN t.send_result <> 'success' THEN 1 ELSE 0 END), 0) AS failed_attempt_count",
	}, ", ")
}

func postSendCountsGroupOrderSQL() string {
	return `
		GROUP BY ` + strings.Join([]string{
		"track.kind",
		"track.channel_id",
		"track.content_id",
		"track.actual_published_at",
		"track.detected_at",
		"track.alarm_sent_at",
		"track.alarm_latency_millis",
		"track.alarm_latency_exceeded",
	}, ", ") + `
		ORDER BY COALESCE(MAX(CASE WHEN t.send_result = 'success' THEN t.event_at END), MAX(t.event_at), track.actual_published_at, track.detected_at) DESC,
		         track.content_id ASC
	`
}

func buildPostSendCountsFromScanRows(scanned []postSendCountScanRow) []PostSendCount {
	rows := make([]PostSendCount, 0, len(scanned))
	for i := range scanned {
		rows = append(rows, PostSendCount{
			OutboxKind:            scanned[i].OutboxKind,
			AlarmType:             scanned[i].AlarmType,
			ChannelID:             scanned[i].ChannelID,
			PostID:                scanned[i].PostID,
			ContentID:             scanned[i].ContentID,
			ActualPublishedAt:     scanned[i].ActualPublishedAt.Ptr(),
			DetectedAt:            scanned[i].DetectedAt.Ptr(),
			AlarmSentAt:           scanned[i].AlarmSentAt.Ptr(),
			AlarmLatencyMillis:    scanned[i].AlarmLatencyMillis,
			AlarmLatencyExceeded:  scanned[i].AlarmLatencyExceeded.Ptr(),
			FirstEventAt:          scanned[i].FirstEventAt.Ptr(),
			LastEventAt:           scanned[i].LastEventAt.Ptr(),
			FirstSuccessAt:        scanned[i].FirstSuccessAt.Ptr(),
			LastSuccessAt:         scanned[i].LastSuccessAt.Ptr(),
			OutboxCount:           scanned[i].OutboxCount,
			SuccessSendCount:      scanned[i].SuccessSendCount,
			SuccessRoomCount:      scanned[i].SuccessRoomCount,
			DuplicateSuccessCount: scanned[i].DuplicateSuccessCount,
			FailedAttemptCount:    scanned[i].FailedAttemptCount,
		})
	}
	return rows
}
