package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func seedRefreshOutbox(ctx context.Context, t *testing.T, counting *execCountingQuerier) int64 {
	t.Helper()
	var outboxID int64
	if err := counting.inner.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload)
		VALUES ('COMMUNITY_POST', 'UC_refresh', 'seed', '{}'::jsonb)
		RETURNING id
	`).Scan(&outboxID); err != nil {
		t.Fatalf("seed parent outbox row: %v", err)
	}
	return outboxID
}

func insertRefreshTelemetryRow(ctx context.Context, t *testing.T, counting *execCountingQuerier, outboxID, deliveryID int64, contentID string) int64 {
	t.Helper()
	var id int64
	if err := counting.inner.QueryRow(ctx, `
		INSERT INTO youtube_notification_delivery_telemetry
			(delivery_id, attempt_ordinal, outbox_id, channel_id, content_id, post_id, room_id,
			 alarm_type, dedupe_key, delivery_mode, send_result, event_at)
		VALUES ($1, 1, $2, 'UC_refresh', $3, $3, 'room-1', $4, $5, 'grouped', 'success', $6)
		RETURNING id
	`, deliveryID, outboxID, contentID, string(domain.AlarmTypeCommunity), "dedupe-"+contentID, time.Now().UTC()).Scan(&id); err != nil {
		t.Fatalf("insert telemetry row %s: %v", contentID, err)
	}
	return id
}

func seedRefreshTracking(ctx context.Context, t *testing.T, counting *execCountingQuerier, contentID string, published, sent, detected time.Time) {
	t.Helper()
	if _, err := counting.inner.Exec(ctx, `
		INSERT INTO youtube_content_alarm_tracking
			(kind, content_id, canonical_content_id, channel_id, actual_published_at, detected_at, alarm_sent_at)
		VALUES ($1, $2, $2, 'UC_refresh', $3, $4, $5)
	`, string(domain.OutboxKindCommunityPost), contentID, published, detected, sent); err != nil {
		t.Fatalf("seed tracking %s: %v", contentID, err)
	}
}

type refreshTelemetryTracking struct {
	actualPublishedAt  *time.Time
	alarmSentAt        *time.Time
	alarmLatencyMillis *int64
	detectedAt         *time.Time
}

func readRefreshTelemetryTracking(ctx context.Context, t *testing.T, counting *execCountingQuerier, id int64) refreshTelemetryTracking {
	t.Helper()
	var got refreshTelemetryTracking
	if err := counting.inner.QueryRow(ctx, `
		SELECT actual_published_at, alarm_sent_at, alarm_latency_millis, detected_at
		FROM youtube_notification_delivery_telemetry
		WHERE id = $1
	`, id).Scan(&got.actualPublishedAt, &got.alarmSentAt, &got.alarmLatencyMillis, &got.detectedAt); err != nil {
		t.Fatalf("read telemetry tracking id=%d: %v", id, err)
	}
	return got
}

func TestRefreshLockedRowsBatchesTargetsAndLeavesOthersUnchanged(t *testing.T) {
	counting := &execCountingQuerier{inner: dbtest.NewPool(t)}
	repo := NewRepository(counting)
	ctx := context.Background()

	outboxID := seedRefreshOutbox(ctx, t, counting)

	idA := insertRefreshTelemetryRow(ctx, t, counting, outboxID, 101, "post-a")
	idB := insertRefreshTelemetryRow(ctx, t, counting, outboxID, 102, "post-b")
	idControl := insertRefreshTelemetryRow(ctx, t, counting, outboxID, 103, "post-control")

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	seedRefreshTracking(ctx, t, counting, "post-a", base, base.Add(90*time.Second), base.Add(10*time.Second))
	seedRefreshTracking(ctx, t, counting, "post-b", base.Add(time.Hour), base.Add(time.Hour+30*time.Second), base.Add(time.Hour+5*time.Second))
	seedRefreshTracking(ctx, t, counting, "post-control", base.Add(-time.Hour), base.Add(-time.Hour+45*time.Second), base.Add(-time.Hour+2*time.Second))

	ctrlPublished := base.Add(-2 * time.Hour)
	ctrlSent := base.Add(-2*time.Hour + 20*time.Second)
	ctrlLatency := int64(20000)
	ctrlDetected := base.Add(-2*time.Hour + 3*time.Second)
	if _, err := counting.inner.Exec(ctx, `
		UPDATE youtube_notification_delivery_telemetry
		SET actual_published_at = $1, alarm_sent_at = $2, alarm_latency_millis = $3, detected_at = $4
		WHERE id = $5
	`, ctrlPublished, ctrlSent, ctrlLatency, ctrlDetected, idControl); err != nil {
		t.Fatalf("seed control baseline: %v", err)
	}

	rows := []domain.YouTubeNotificationDeliveryTelemetry{
		{ID: idA, AlarmType: domain.AlarmTypeCommunity, ContentID: "post-a"},
		{ID: idB, AlarmType: domain.AlarmTypeCommunity, ContentID: "post-b"},
	}

	counting.execCalls = 0
	if err := repo.refreshLockedRows(ctx, rows); err != nil {
		t.Fatalf("refreshLockedRows() error = %v", err)
	}

	if counting.execCalls != 1 {
		t.Fatalf("refreshLockedRows exec round-trips = %d for %d changed rows, want 1 (batched)", counting.execCalls, len(rows))
	}

	if rows[0].ActualPublishedAt == nil || rows[0].AlarmSentAt == nil || rows[0].AlarmLatencyMillis == nil || rows[0].DetectedAt == nil {
		t.Fatalf("enriched row A has nil tracking field: %+v", rows[0])
	}
	if rows[1].AlarmLatencyMillis == nil || *rows[0].AlarmLatencyMillis == *rows[1].AlarmLatencyMillis {
		t.Fatalf("rows A/B latency not distinct: A=%v B=%v", rows[0].AlarmLatencyMillis, rows[1].AlarmLatencyMillis)
	}

	gotA := readRefreshTelemetryTracking(ctx, t, counting, idA)
	if !sameUTCTimePtr(gotA.actualPublishedAt, rows[0].ActualPublishedAt) ||
		!sameUTCTimePtr(gotA.alarmSentAt, rows[0].AlarmSentAt) ||
		!sameInt64Ptr(gotA.alarmLatencyMillis, rows[0].AlarmLatencyMillis) ||
		!sameUTCTimePtr(gotA.detectedAt, rows[0].DetectedAt) {
		t.Fatalf("row A persisted = %+v, want enriched %+v", gotA, rows[0])
	}

	gotB := readRefreshTelemetryTracking(ctx, t, counting, idB)
	if !sameUTCTimePtr(gotB.actualPublishedAt, rows[1].ActualPublishedAt) ||
		!sameUTCTimePtr(gotB.alarmSentAt, rows[1].AlarmSentAt) ||
		!sameInt64Ptr(gotB.alarmLatencyMillis, rows[1].AlarmLatencyMillis) ||
		!sameUTCTimePtr(gotB.detectedAt, rows[1].DetectedAt) {
		t.Fatalf("row B persisted = %+v, want enriched %+v", gotB, rows[1])
	}

	gotControl := readRefreshTelemetryTracking(ctx, t, counting, idControl)
	if !sameUTCTimePtr(gotControl.actualPublishedAt, &ctrlPublished) ||
		!sameUTCTimePtr(gotControl.alarmSentAt, &ctrlSent) ||
		!sameInt64Ptr(gotControl.alarmLatencyMillis, &ctrlLatency) ||
		!sameUTCTimePtr(gotControl.detectedAt, &ctrlDetected) {
		t.Fatalf("control row mutated = %+v, want baseline (published=%v)", gotControl, ctrlPublished)
	}

	if err := repo.refreshLockedRows(ctx, rows); err != nil {
		t.Fatalf("second refreshLockedRows() error = %v", err)
	}
	if counting.execCalls != 1 {
		t.Fatalf("second refreshLockedRows issued extra exec: total = %d, want 1 (no update when unchanged)", counting.execCalls)
	}
}
