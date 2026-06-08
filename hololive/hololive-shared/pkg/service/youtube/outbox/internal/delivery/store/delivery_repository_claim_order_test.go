package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFetchAndLockClaimsEarliestNextAttemptBeforeEarliestCreated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := dbtest.NewPool(t)
	repo := NewDeliveryRepository(pool, nil)

	now := time.Now().UTC().Truncate(time.Microsecond)
	outboxID := insertClaimOrderOutbox(t, ctx, pool, now)

	createdFirstID := insertClaimOrderDelivery(t, ctx, pool, outboxID, "room-created-first", now.Add(-20*time.Minute), now.Add(-1*time.Minute))
	dueFirstID := insertClaimOrderDelivery(t, ctx, pool, outboxID, "room-due-first", now.Add(-5*time.Minute), now.Add(-10*time.Minute))

	rows, err := repo.FetchAndLock(ctx, 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, dueFirstID, rows[0].ID)
	require.NotEqual(t, createdFirstID, rows[0].ID)
}

func insertClaimOrderOutbox(t *testing.T, ctx context.Context, db *pgxpool.Pool, now time.Time) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(ctx, `
        INSERT INTO youtube_notification_outbox
            (kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
        VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8)
        RETURNING id
    `, domain.OutboxKindCommunityPost, "UC_claim_order", "post-claim-order", `{"post_id":"post-claim-order"}`, domain.OutboxStatusPending, 0, now.Add(-10*time.Minute), now.Add(-30*time.Minute)).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertClaimOrderDelivery(t *testing.T, ctx context.Context, db *pgxpool.Pool, outboxID int64, roomID string, createdAt time.Time, nextAttemptAt time.Time) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(ctx, `
        INSERT INTO youtube_notification_delivery
            (outbox_id, room_id, status, attempt_count, next_attempt_at, created_at)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id
    `, outboxID, roomID, domain.OutboxStatusPending, 0, nextAttemptAt, createdAt).Scan(&id)
	require.NoError(t, err)
	return id
}
