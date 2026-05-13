package outbox

import (
	"context"
	"fmt"
	"strings"
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

type postSendCountScanRow struct {
	OutboxKind            domain.OutboxKind `gorm:"column:outbox_kind"`
	AlarmType             domain.AlarmType  `gorm:"column:alarm_type"`
	ChannelID             string            `gorm:"column:channel_id"`
	PostID                string            `gorm:"column:post_id"`
	ContentID             string            `gorm:"column:content_id"`
	ActualPublishedAt     scannableTime     `gorm:"column:actual_published_at"`
	DetectedAt            scannableTime     `gorm:"column:detected_at"`
	AlarmSentAt           scannableTime     `gorm:"column:alarm_sent_at"`
	AlarmLatencyMillis    *int64            `gorm:"column:alarm_latency_millis"`
	AlarmLatencyExceeded  scannableBool     `gorm:"column:alarm_latency_exceeded"`
	FirstEventAt          scannableTime     `gorm:"column:first_event_at"`
	LastEventAt           scannableTime     `gorm:"column:last_event_at"`
	FirstSuccessAt        scannableTime     `gorm:"column:first_success_at"`
	LastSuccessAt         scannableTime     `gorm:"column:last_success_at"`
	OutboxCount           int64             `gorm:"column:outbox_count"`
	SuccessSendCount      int64             `gorm:"column:success_send_count"`
	SuccessRoomCount      int64             `gorm:"column:success_room_count"`
	DuplicateSuccessCount int64             `gorm:"column:duplicate_success_count"`
	FailedAttemptCount    int64             `gorm:"column:failed_attempt_count"`
}

func (r *DeliveryTelemetryRepository) ListPostSendCountsSince(ctx context.Context, since time.Time) ([]PostSendCount, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post send counts since: db is nil")
	}
	if since.IsZero() {
		return nil, fmt.Errorf("list post send counts since: since is empty")
	}

	rows, err := r.listPostSendCounts(ctx, since.UTC(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("list post send counts since: %w", err)
	}
	return rows, nil
}

func (r *DeliveryTelemetryRepository) ListPostSendCountsWithinPublishedWindow(
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

	rows, err := r.listPostSendCounts(ctx, startUTC, &endUTC, nil)
	if err != nil {
		return nil, fmt.Errorf("list post send counts within published window: %w", err)
	}
	return rows, nil
}

func (r *DeliveryTelemetryRepository) ListPostSendCountsWithinObservationWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
	detectedBefore time.Time,
) ([]PostSendCount, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post send counts within observation window: db is nil")
	}
	if windowStart.IsZero() {
		return nil, fmt.Errorf("list post send counts within observation window: window start is empty")
	}
	if windowEnd.IsZero() {
		return nil, fmt.Errorf("list post send counts within observation window: window end is empty")
	}
	if detectedBefore.IsZero() {
		return nil, fmt.Errorf("list post send counts within observation window: detected before is empty")
	}

	startUTC := windowStart.UTC()
	endUTC := windowEnd.UTC()
	detectedBeforeUTC := detectedBefore.UTC()
	if !startUTC.Before(endUTC) {
		return nil, fmt.Errorf("list post send counts within observation window: window start must be before window end")
	}
	if detectedBeforeUTC.Before(endUTC) {
		return nil, fmt.Errorf("list post send counts within observation window: detected before must be on or after window end")
	}

	rows, err := r.listPostSendCounts(ctx, startUTC, &endUTC, &detectedBeforeUTC)
	if err != nil {
		return nil, fmt.Errorf("list post send counts within observation window: %w", err)
	}
	return rows, nil
}

func (r *DeliveryTelemetryRepository) ListPostSendCountsByFinalizedObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
) ([]PostSendCount, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list post send counts by finalized observation window: db is nil")
	}

	normalizedRuntimeName := strings.TrimSpace(runtimeName)
	if normalizedRuntimeName == "" {
		return nil, fmt.Errorf("list post send counts by finalized observation window: runtime name is empty")
	}
	if bigBangCutoverAt.IsZero() {
		return nil, fmt.Errorf("list post send counts by finalized observation window: big-bang cutover at is empty")
	}

	var scanned []postSendCountScanRow
	query := r.db.WithContext(ctx).
		Table("youtube_community_shorts_observation_post_baselines AS base").
		Select(finalizedObservationPostSendCountsSelectSQL()).
		Joins("LEFT JOIN youtube_content_alarm_tracking track ON track.kind = base.kind AND track.canonical_content_id = base.post_id").
		Joins("LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id").
		Joins("LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id").
		Where("base.runtime_name = ?", normalizedRuntimeName).
		Where("base.bigbang_cutover_at = ?", bigBangCutoverAt.UTC()).
		Group(finalizedObservationPostSendCountsGroupSQL()).
		Order("COALESCE(MAX(CASE WHEN t.send_result = 'success' THEN t.event_at END), MAX(t.event_at), track.actual_published_at, base.actual_published_at, track.detected_at, base.detected_at) DESC").
		Order("base.post_id ASC")
	if err := query.Scan(&scanned).Error; err != nil {
		return nil, fmt.Errorf("list post send counts by finalized observation window: scan rows: %w", err)
	}

	return buildPostSendCountsFromScanRows(scanned), nil
}

func finalizedObservationPostSendCountsSelectSQL() string {
	return strings.Join([]string{
		"base.kind AS outbox_kind",
		"CASE base.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' WHEN 'NEW_SHORT' THEN 'SHORTS' ELSE 'LIVE' END AS alarm_type",
		"COALESCE(track.channel_id, base.channel_id) AS channel_id",
		"base.post_id AS post_id",
		"COALESCE(track.content_id, base.post_id) AS content_id",
		"COALESCE(track.actual_published_at, base.actual_published_at) AS actual_published_at",
		"COALESCE(track.detected_at, base.detected_at) AS detected_at",
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

func finalizedObservationPostSendCountsGroupSQL() string {
	return strings.Join([]string{
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
	}, ", ")
}

func (r *DeliveryTelemetryRepository) listPostSendCounts(
	ctx context.Context,
	windowStart time.Time,
	windowEnd *time.Time,
	detectedBefore *time.Time,
) ([]PostSendCount, error) {
	var scanned []postSendCountScanRow
	query := r.db.WithContext(ctx).
		Table("youtube_content_alarm_tracking AS track").
		Select(strings.Join([]string{
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
		}, ", ")).
		Joins("LEFT JOIN youtube_notification_outbox o ON o.kind = track.kind AND o.content_id = track.content_id").
		Joins("LEFT JOIN youtube_notification_delivery_telemetry t ON t.outbox_id = o.id AND t.event_at >= ?", windowStart.UTC()).
		Where("track.kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
		Where("COALESCE(track.actual_published_at, track.detected_at) >= ?", windowStart.UTC())
	if windowEnd != nil {
		query = query.Where("COALESCE(track.actual_published_at, track.detected_at) < ?", windowEnd.UTC())
	}
	if detectedBefore != nil {
		query = query.Where("track.detected_at < ?", detectedBefore.UTC())
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
	}, ", ")).
		Order("COALESCE(MAX(CASE WHEN t.send_result = 'success' THEN t.event_at END), MAX(t.event_at), track.actual_published_at, track.detected_at) DESC").
		Order("track.content_id ASC")
	if err := query.Scan(&scanned).Error; err != nil {
		return nil, fmt.Errorf("scan rows: %w", err)
	}

	return buildPostSendCountsFromScanRows(scanned), nil
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
