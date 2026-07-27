package telemetry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type execCountingQuerier struct {
	inner     dbx.Querier
	execCalls int
}

func (c *execCountingQuerier) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.execCalls++
	return c.inner.Exec(ctx, sql, args...)
}

func (c *execCountingQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return c.inner.Query(ctx, sql, args...)
}

func (c *execCountingQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return c.inner.QueryRow(ctx, sql, args...)
}

func newTelemetryEnqueueTestRepo(t *testing.T) (*Repository, *execCountingQuerier, int64) {
	t.Helper()

	pool := dbtest.NewPool(t)

	var outboxID int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload)
		VALUES ('COMMUNITY_POST', 'UC_test_channel', 'content-1', '{}'::jsonb)
		RETURNING id
	`).Scan(&outboxID); err != nil {
		t.Fatalf("seed parent outbox row: %v", err)
	}

	counting := &execCountingQuerier{inner: pool}
	return NewRepository(counting), counting, outboxID
}

func makeEnqueueTestRow(outboxID, deliveryID int64, ordinal int) domain.YouTubeNotificationDeliveryTelemetry {
	now := time.Now().UTC().Truncate(time.Millisecond)
	latency := int64(1500)
	sentAt := now.Add(-time.Minute)
	return domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:         deliveryID,
		AttemptOrdinal:     ordinal,
		OutboxID:           outboxID,
		ChannelID:          "UC_test_channel",
		ContentID:          "content-1",
		PostID:             "post-1",
		RoomID:             "room-1",
		AlarmType:          domain.AlarmTypeCommunity,
		AlarmSentAt:        &sentAt,
		AlarmLatencyMillis: &latency,
		DedupeKey:          "dedupe-key",
		DeliveryPath:       "community_post",
		DeliveryMode:       "grouped",
		SendResult:         "pending",
		EventAt:            now,
		NextAttemptAt:      now,
		Error:              "",
	}
}

func TestEnqueuePreparedInsertsBatchInSingleRoundTrip(t *testing.T) {
	repo, counting, outboxID := newTelemetryEnqueueTestRepo(t)
	ctx := context.Background()

	rows := []domain.YouTubeNotificationDeliveryTelemetry{
		makeEnqueueTestRow(outboxID, 101, 1),
		makeEnqueueTestRow(outboxID, 102, 1),
		makeEnqueueTestRow(outboxID, 103, 2),
	}

	if err := repo.EnqueuePrepared(ctx, rows); err != nil {
		t.Fatalf("EnqueuePrepared() error = %v", err)
	}

	if counting.execCalls != 1 {
		t.Fatalf("EnqueuePrepared exec round-trips = %d for %d rows, want 1", counting.execCalls, len(rows))
	}

	var count int
	if err := counting.inner.QueryRow(ctx, `SELECT COUNT(*) FROM youtube_notification_delivery_telemetry`).Scan(&count); err != nil {
		t.Fatalf("count telemetry rows: %v", err)
	}
	if count != len(rows) {
		t.Fatalf("persisted rows = %d, want %d", count, len(rows))
	}

	var gotChannelID, gotPostID string
	var gotLatency *int64
	var gotSentAt *time.Time
	if err := counting.inner.QueryRow(ctx, `
		SELECT channel_id, post_id, alarm_latency_millis, alarm_sent_at
		FROM youtube_notification_delivery_telemetry
		WHERE delivery_id = 101 AND attempt_ordinal = 1
	`).Scan(&gotChannelID, &gotPostID, &gotLatency, &gotSentAt); err != nil {
		t.Fatalf("load persisted telemetry row: %v", err)
	}
	if gotChannelID != "UC_test_channel" || gotPostID != "post-1" {
		t.Fatalf("persisted row = (%q, %q), want (UC_test_channel, post-1)", gotChannelID, gotPostID)
	}
	if gotLatency == nil || *gotLatency != 1500 {
		t.Fatalf("persisted alarm_latency_millis = %v, want 1500", gotLatency)
	}
	if gotSentAt == nil {
		t.Fatal("persisted alarm_sent_at = nil, want non-nil")
	}
}

func TestEnqueuePreparedChunkBoundaries(t *testing.T) {
	repo, counting, outboxID := newTelemetryEnqueueTestRepo(t)
	ctx := context.Background()

	for _, size := range []int{1, 499, 500, 501, 1001} {
		if _, err := counting.inner.Exec(ctx, `TRUNCATE youtube_notification_delivery_telemetry`); err != nil {
			t.Fatalf("truncate telemetry rows: %v", err)
		}
		counting.execCalls = 0

		rows := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, size)
		for i := range size {
			rows = append(rows, makeEnqueueTestRow(outboxID, int64(1000+i), 1))
		}

		if err := repo.EnqueuePrepared(ctx, rows); err != nil {
			t.Fatalf("EnqueuePrepared(size=%d) error = %v", size, err)
		}

		wantChunks := (size + enqueueTelemetryChunkSize - 1) / enqueueTelemetryChunkSize
		if counting.execCalls != wantChunks {
			t.Fatalf("EnqueuePrepared(size=%d) exec round-trips = %d, want %d", size, counting.execCalls, wantChunks)
		}

		var count int
		if err := counting.inner.QueryRow(ctx, `SELECT COUNT(*) FROM youtube_notification_delivery_telemetry`).Scan(&count); err != nil {
			t.Fatalf("count telemetry rows (size=%d): %v", size, err)
		}
		if count != size {
			t.Fatalf("persisted rows (size=%d) = %d, want %d", size, count, size)
		}
	}
}

func TestEnqueuePreparedSkipsDuplicateWithinSingleBatch(t *testing.T) {
	repo, counting, outboxID := newTelemetryEnqueueTestRepo(t)
	ctx := context.Background()

	rows := []domain.YouTubeNotificationDeliveryTelemetry{
		makeEnqueueTestRow(outboxID, 301, 1),
		makeEnqueueTestRow(outboxID, 301, 1),
		makeEnqueueTestRow(outboxID, 302, 1),
	}

	if err := repo.EnqueuePrepared(ctx, rows); err != nil {
		t.Fatalf("EnqueuePrepared(duplicate within batch) error = %v", err)
	}

	var count int
	if err := counting.inner.QueryRow(ctx, `SELECT COUNT(*) FROM youtube_notification_delivery_telemetry`).Scan(&count); err != nil {
		t.Fatalf("count telemetry rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("persisted rows with intra-batch duplicate = %d, want 2", count)
	}
}

func TestTelemetrySurvivesOutboxRetentionDelete(t *testing.T) {
	repo, counting, outboxID := newTelemetryEnqueueTestRepo(t)
	ctx := context.Background()

	if err := repo.EnqueuePrepared(ctx, []domain.YouTubeNotificationDeliveryTelemetry{
		makeEnqueueTestRow(outboxID, 401, 1),
	}); err != nil {
		t.Fatalf("EnqueuePrepared() error = %v", err)
	}

	if _, err := counting.inner.Exec(ctx, `DELETE FROM youtube_notification_outbox WHERE id = $1`, outboxID); err != nil {
		t.Fatalf("delete source outbox row: %v", err)
	}

	var count int
	if err := counting.inner.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM youtube_notification_delivery_telemetry
		WHERE outbox_id = $1
	`, outboxID).Scan(&count); err != nil {
		t.Fatalf("count telemetry rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("telemetry rows after source outbox delete = %d, want 1", count)
	}
}

// chunk 단위 원자성은 의도된 계약이다: chunk 내 비-conflict 오류는 해당 chunk를
// 통째로 롤백하고(이미 영속된 이전 chunk는 유지) 즉시 반환한다.
func TestEnqueuePreparedChunkFailureRollsBackOnlyThatChunk(t *testing.T) {
	repo, counting, outboxID := newTelemetryEnqueueTestRepo(t)
	ctx := context.Background()

	size := enqueueTelemetryChunkSize + 2
	rows := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, size)
	for i := range size {
		rows = append(rows, makeEnqueueTestRow(outboxID, int64(2000+i), 1))
	}
	rows[size-1].ContentID = strings.Repeat("x", 51)

	if err := repo.EnqueuePrepared(ctx, rows); err == nil {
		t.Fatal("EnqueuePrepared with overlong content_id row = nil error, want error")
	}

	var count int
	if err := counting.inner.QueryRow(ctx, `SELECT COUNT(*) FROM youtube_notification_delivery_telemetry`).Scan(&count); err != nil {
		t.Fatalf("count telemetry rows: %v", err)
	}
	if count != enqueueTelemetryChunkSize {
		t.Fatalf("persisted rows after failing second chunk = %d, want %d (first chunk only)", count, enqueueTelemetryChunkSize)
	}
}

func TestEnqueuePreparedSkipsConflictsWithinBatch(t *testing.T) {
	repo, counting, outboxID := newTelemetryEnqueueTestRepo(t)
	ctx := context.Background()

	first := []domain.YouTubeNotificationDeliveryTelemetry{
		makeEnqueueTestRow(outboxID, 201, 1),
		makeEnqueueTestRow(outboxID, 202, 1),
	}
	if err := repo.EnqueuePrepared(ctx, first); err != nil {
		t.Fatalf("EnqueuePrepared(first) error = %v", err)
	}

	second := []domain.YouTubeNotificationDeliveryTelemetry{
		makeEnqueueTestRow(outboxID, 201, 1),
		makeEnqueueTestRow(outboxID, 203, 1),
	}
	if err := repo.EnqueuePrepared(ctx, second); err != nil {
		t.Fatalf("EnqueuePrepared(second) error = %v", err)
	}

	var count int
	if err := counting.inner.QueryRow(ctx, `SELECT COUNT(*) FROM youtube_notification_delivery_telemetry`).Scan(&count); err != nil {
		t.Fatalf("count telemetry rows: %v", err)
	}
	if count != 3 {
		t.Fatalf("persisted rows after duplicate batch = %d, want 3", count)
	}
}
