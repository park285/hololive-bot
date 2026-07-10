package store

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func seedAggregateOutbox(t *testing.T, ctx context.Context, db *pgxpool.Pool, contentID string, outboxStatus domain.OutboxStatus, deliveryStatuses []domain.OutboxStatus) int64 {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Microsecond)
	var outboxID int64
	err := db.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at, locked_at)
		VALUES ($1, $2, $3, '{}'::jsonb, $4, 0, $5, $5, $5)
		RETURNING id
	`, string(domain.OutboxKindNewVideo), "channel-agg", contentID, string(outboxStatus), now).Scan(&outboxID)
	require.NoError(t, err)

	for i, status := range deliveryStatuses {
		_, err := db.Exec(ctx, `
			INSERT INTO youtube_notification_delivery
				(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at)
			VALUES ($1, $2, $3, 0, $4, $4)
		`, outboxID, "room-agg-"+string(rune('a'+i)), string(status), now)
		require.NoError(t, err)
	}
	return outboxID
}

func readOutboxAggregateRow(t *testing.T, ctx context.Context, db *pgxpool.Pool, outboxID int64) (outboxStatus domain.OutboxStatus, sentAtValue, lockedAtValue *time.Time, errorText string) {
	t.Helper()

	var status domain.OutboxStatus
	var sentAt, lockedAt *time.Time
	var errText string
	err := db.QueryRow(ctx, `
		SELECT status, sent_at, locked_at, COALESCE(error, '')
		FROM youtube_notification_outbox
		WHERE id = $1
	`, outboxID).Scan(&status, &sentAt, &lockedAt, &errText)
	require.NoError(t, err)
	return status, sentAt, lockedAt, errText
}

func TestUpdateOutboxAggregateStatuses_AllSentMarksSentOnce(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(t, ctx, pool, "agg-sent", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusSent})

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, sentAt, lockedAt, errText := readOutboxAggregateRow(t, ctx, pool, outboxID)
	require.Equal(t, domain.OutboxStatusSent, status)
	require.NotNil(t, sentAt)
	require.Nil(t, lockedAt)
	require.Empty(t, errText)
}

func TestUpdateOutboxAggregateStatuses_DoesNotRewriteExistingSentAt(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(t, ctx, pool, "agg-sent-keep", domain.OutboxStatusSent,
		[]domain.OutboxStatus{domain.OutboxStatusSent})

	firstSentAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	_, err := pool.Exec(ctx,
		"UPDATE youtube_notification_outbox SET sent_at = $1 WHERE id = $2", firstSentAt, outboxID)
	require.NoError(t, err)

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	_, sentAt, _, _ := readOutboxAggregateRow(t, ctx, pool, outboxID)
	require.NotNil(t, sentAt)
	require.True(t, sentAt.UTC().Equal(firstSentAt),
		"reconcile/aggregate-sync 재실행이 sent_at을 재기록하면 latency 통계가 오염된다: got %s", sentAt)
}

func TestUpdateOutboxAggregateStatuses_FailedWinsOverSent(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(t, ctx, pool, "agg-failed", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusFailed})

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, sentAt, _, errText := readOutboxAggregateRow(t, ctx, pool, outboxID)
	require.Equal(t, domain.OutboxStatusFailed, status)
	require.Nil(t, sentAt)
	require.Equal(t, "per-room delivery failed", errText)
}

func TestUpdateOutboxAggregateStatuses_PendingDeliveryKeepsOutboxPending(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(t, ctx, pool, "agg-pending", domain.OutboxStatusSent,
		[]domain.OutboxStatus{domain.OutboxStatusSent, domain.OutboxStatusPending})

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, _, _, _ := readOutboxAggregateRow(t, ctx, pool, outboxID)
	require.Equal(t, domain.OutboxStatusPending, status)
}

func TestUpdateOutboxAggregateStatuses_NoDeliveriesDefaultsToPending(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(t, ctx, pool, "agg-empty", domain.OutboxStatusSent, nil)

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	status, _, _, _ := readOutboxAggregateRow(t, ctx, pool, outboxID)
	require.Equal(t, domain.OutboxStatusPending, status)
}

func TestUpdateOutboxAggregateStatuses_UnchangedStatusIsNoOpWrite(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	outboxID := seedAggregateOutbox(t, ctx, pool, "agg-noop", domain.OutboxStatusPending,
		[]domain.OutboxStatus{domain.OutboxStatusPending})

	var xminBefore uint32
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT xmin FROM youtube_notification_outbox WHERE id = $1", outboxID).Scan(&xminBefore))

	require.NoError(t, repository.UpdateOutboxAggregateStatuses(ctx, []int64{outboxID}))

	var xminAfter uint32
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT xmin FROM youtube_notification_outbox WHERE id = $1", outboxID).Scan(&xminAfter))
	require.Equal(t, xminBefore, xminAfter,
		"IS DISTINCT FROM 가드: 상태가 같으면 새 row version(dead tuple)을 만들지 않아야 한다")
}
