package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store"
)

func TestFetchAndLockForPerRoomReturnsByNextAttemptBeforeCreated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	createdFirstID := insertClaimOrderOutboxForDispatch(t, ctx, pool, "created-first", now.Add(-20*time.Minute), now.Add(-1*time.Minute))
	dueFirstID := insertClaimOrderOutboxForDispatch(t, ctx, pool, "due-first", now.Add(-5*time.Minute), now.Add(-10*time.Minute))

	manager := &ClaimManager{
		db:       pool,
		config:   Config{BatchSize: 1, LockTimeout: time.Minute},
		delivery: store.NewDeliveryRepository(pool, nil),
	}
	rows, err := manager.fetchAndLockForPerRoom(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, dueFirstID, rows[0].ID)
	require.NotEqual(t, createdFirstID, rows[0].ID)
}

func TestFetchAndLockForPerRoomSkipsStaleByClaimFreshnessWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	freshID := insertClaimOrderOutboxForDispatch(t, ctx, pool, "fresh-within-window", now.Add(-30*time.Minute), now.Add(-1*time.Minute))
	staleID := insertClaimOrderOutboxForDispatch(t, ctx, pool, "stale-beyond-window", now.Add(-3*time.Hour), now.Add(-1*time.Minute))

	manager := &ClaimManager{
		db:       pool,
		config:   Config{BatchSize: 10, LockTimeout: time.Minute, ClaimFreshnessWindow: 2 * time.Hour},
		delivery: store.NewDeliveryRepository(pool, nil),
	}
	rows, err := manager.fetchAndLockForPerRoom(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, freshID, rows[0].ID)
	require.NotEqual(t, staleID, rows[0].ID)
}

func TestFetchAndLockForPerRoomClaimsStaleWhenFreshnessWindowDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	staleID := insertClaimOrderOutboxForDispatch(t, ctx, pool, "stale-no-window", now.Add(-72*time.Hour), now.Add(-1*time.Minute))

	manager := &ClaimManager{
		db:       pool,
		config:   Config{BatchSize: 10, LockTimeout: time.Minute},
		delivery: store.NewDeliveryRepository(pool, nil),
	}
	rows, err := manager.fetchAndLockForPerRoom(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, staleID, rows[0].ID)
}

func insertClaimOrderOutboxForDispatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, contentID string, createdAt, nextAttemptAt time.Time) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx, `
        INSERT INTO youtube_notification_outbox
            (kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
        VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8)
        RETURNING id
    `, domain.OutboxKindCommunityPost, "UC_dispatch_claim_order", contentID, `{"post_id":"`+contentID+`"}`, domain.OutboxStatusPending, 0, nextAttemptAt, createdAt).Scan(&id)
	require.NoError(t, err)
	return id
}
