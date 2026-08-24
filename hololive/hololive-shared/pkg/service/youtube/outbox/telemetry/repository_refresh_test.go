package telemetry

import (
	"context"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
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

func refreshTrackingOf(row *domain.YouTubeNotificationDeliveryTelemetry) refreshTelemetryTracking {
	return refreshTelemetryTracking{
		actualPublishedAt:  row.ActualPublishedAt,
		alarmSentAt:        row.AlarmSentAt,
		alarmLatencyMillis: row.AlarmLatencyMillis,
		detectedAt:         row.DetectedAt,
	}
}

func requireRefreshTelemetryTracking(ctx context.Context, t *testing.T, counting *execCountingQuerier, id int64, want refreshTelemetryTracking, label string) {
	t.Helper()

	got := readRefreshTelemetryTracking(ctx, t, counting, id)
	if !sameUTCTimePtr(got.actualPublishedAt, want.actualPublishedAt) ||
		!sameUTCTimePtr(got.alarmSentAt, want.alarmSentAt) ||
		!sameInt64Ptr(got.alarmLatencyMillis, want.alarmLatencyMillis) ||
		!sameUTCTimePtr(got.detectedAt, want.detectedAt) {
		t.Fatalf("%s persisted = %+v, want %+v", label, got, want)
	}
}

func requireRefreshRowsEnriched(t *testing.T, rows []domain.YouTubeNotificationDeliveryTelemetry) {
	t.Helper()

	if rows[0].ActualPublishedAt == nil || rows[0].AlarmSentAt == nil || rows[0].AlarmLatencyMillis == nil || rows[0].DetectedAt == nil {
		t.Fatalf("enriched row A has nil tracking field: %+v", rows[0])
	}

	if rows[1].AlarmLatencyMillis == nil || *rows[0].AlarmLatencyMillis == *rows[1].AlarmLatencyMillis {
		t.Fatalf("rows A/B latency not distinct: A=%v B=%v", rows[0].AlarmLatencyMillis, rows[1].AlarmLatencyMillis)
	}
}

func seedRefreshControlBaseline(ctx context.Context, t *testing.T, counting *execCountingQuerier, id int64, base time.Time) refreshTelemetryTracking {
	t.Helper()

	published := base.Add(-2 * time.Hour)
	sent := base.Add(-2*time.Hour + 20*time.Second)
	detected := base.Add(-2*time.Hour + 3*time.Second)
	latency := int64(20000)

	if _, err := counting.inner.Exec(ctx, `
		UPDATE youtube_notification_delivery_telemetry
		SET actual_published_at = $1, alarm_sent_at = $2, alarm_latency_millis = $3, detected_at = $4
		WHERE id = $5
	`, published, sent, latency, detected, id); err != nil {
		t.Fatalf("seed control baseline: %v", err)
	}

	return refreshTelemetryTracking{
		actualPublishedAt:  &published,
		alarmSentAt:        &sent,
		alarmLatencyMillis: &latency,
		detectedAt:         &detected,
	}
}

func TestRefreshLockedRowsBatchesTargetsAndLeavesOthersUnchanged(t *testing.T) {
	counting := &execCountingQuerier{inner: dbtest.NewPool(t)}
	repo := NewRepository(counting)
	ctx := t.Context()
	base := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	outboxID := seedRefreshOutbox(ctx, t, counting)
	idA := insertRefreshTelemetryRow(ctx, t, counting, outboxID, 101, "post-a")
	idB := insertRefreshTelemetryRow(ctx, t, counting, outboxID, 102, "post-b")
	idControl := insertRefreshTelemetryRow(ctx, t, counting, outboxID, 103, "post-control")

	seedRefreshTracking(ctx, t, counting, "post-a", base, base.Add(90*time.Second), base.Add(10*time.Second))
	seedRefreshTracking(ctx, t, counting, "post-b", base.Add(time.Hour), base.Add(time.Hour+30*time.Second), base.Add(time.Hour+5*time.Second))
	seedRefreshTracking(ctx, t, counting, "post-control", base.Add(-time.Hour), base.Add(-time.Hour+45*time.Second), base.Add(-time.Hour+2*time.Second))

	control := seedRefreshControlBaseline(ctx, t, counting, idControl, base)

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

	requireRefreshRowsEnriched(t, rows)
	requireRefreshTelemetryTracking(ctx, t, counting, idA, refreshTrackingOf(&rows[0]), "row A")
	requireRefreshTelemetryTracking(ctx, t, counting, idB, refreshTrackingOf(&rows[1]), "row B")
	requireRefreshTelemetryTracking(ctx, t, counting, idControl, control, "control row")

	if err := repo.refreshLockedRows(ctx, rows); err != nil {
		t.Fatalf("second refreshLockedRows() error = %v", err)
	}

	if counting.execCalls != 1 {
		t.Fatalf("second refreshLockedRows issued extra exec: total = %d, want 1 (no update when unchanged)", counting.execCalls)
	}
}
