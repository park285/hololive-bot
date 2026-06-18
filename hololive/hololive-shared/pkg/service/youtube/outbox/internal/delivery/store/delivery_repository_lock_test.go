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

func TestMarkSentBatchIfLockedRejectsStaleRelockWithinOneMillisecond(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	repository := NewDeliveryRepository(pool, slog.New(slog.DiscardHandler))
	staleLockedAt := time.Date(2026, time.June, 3, 12, 0, 0, 123456000, time.UTC)
	currentLockedAt := staleLockedAt.Add(500 * time.Microsecond)
	deliveryID := seedLockedDelivery(t, ctx, pool, staleLockedAt)

	_, err := pool.Exec(ctx, `
		UPDATE youtube_notification_delivery
		SET locked_at = $1
		WHERE id = $2
	`, currentLockedAt, deliveryID)
	require.NoError(t, err)

	err = repository.MarkSentBatchIfLocked(ctx, []LockToken{NewLockToken(deliveryID, &staleLockedAt)})
	require.NoError(t, err)

	status, lockedAt, sentAt := readDeliveryStatusAndLocks(t, ctx, pool, deliveryID)
	require.Equal(t, domain.OutboxStatusPending, status)
	require.NotNil(t, lockedAt)
	require.True(t, lockedAt.Equal(currentLockedAt), "locked_at = %s, want %s", lockedAt, currentLockedAt)
	require.Nil(t, sentAt)

	err = repository.MarkSentBatchIfLocked(ctx, []LockToken{NewLockToken(deliveryID, &currentLockedAt)})
	require.NoError(t, err)

	status, lockedAt, sentAt = readDeliveryStatusAndLocks(t, ctx, pool, deliveryID)
	require.Equal(t, domain.OutboxStatusSent, status)
	require.Nil(t, lockedAt)
	require.NotNil(t, sentAt)
}

func seedLockedDelivery(t *testing.T, ctx context.Context, db *pgxpool.Pool, lockedAt time.Time) int64 {
	t.Helper()

	var outboxID int64
	err := db.QueryRow(ctx, `
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, 0, $6, $7)
		RETURNING id
	`, string(domain.OutboxKindNewVideo), "channel-lock-race", "video-lock-race", "{}", string(domain.OutboxStatusPending), lockedAt, lockedAt).Scan(&outboxID)
	require.NoError(t, err)

	var deliveryID int64
	err = db.QueryRow(ctx, `
		INSERT INTO youtube_notification_delivery
			(outbox_id, room_id, status, attempt_count, next_attempt_at, created_at, locked_at)
		VALUES ($1, $2, $3, 0, $4, $5, $6)
		RETURNING id
	`, outboxID, "room-lock-race", string(domain.OutboxStatusPending), lockedAt, lockedAt, lockedAt).Scan(&deliveryID)
	require.NoError(t, err)

	return deliveryID
}

func readDeliveryStatusAndLocks(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	deliveryID int64,
) (result1 domain.OutboxStatus, result2, result3 *time.Time) {
	t.Helper()

	var status domain.OutboxStatus
	var lockedAt *time.Time
	var sentAt *time.Time
	err := db.QueryRow(ctx, `
		SELECT status, locked_at, sent_at
		FROM youtube_notification_delivery
		WHERE id = $1
	`, deliveryID).Scan(&status, &lockedAt, &sentAt)
	require.NoError(t, err)
	return status, lockedAt, sentAt
}
