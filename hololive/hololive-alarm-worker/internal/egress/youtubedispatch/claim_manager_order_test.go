package youtubedispatch

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestClaimOutboxesForFanoutReturnsByNextAttemptBeforeCreated(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newDeliveryPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	createdFirstID := insertClaimOrderOutboxForDispatch(t, pool, "created-first", now.Add(-20*time.Minute), now.Add(-1*time.Minute))
	dueFirstID := insertClaimOrderOutboxForDispatch(t, pool, "due-first", now.Add(-5*time.Minute), now.Add(-10*time.Minute))

	transition := claimOrderTransitionStore(t, pool)
	rows, err := transition.ClaimOutboxesForFanout(ctx, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, dueFirstID, rows[0].ID)
	require.NotEqual(t, createdFirstID, rows[0].ID)
}

func TestClaimOutboxesForFanoutSkipsStaleByClaimFreshnessWindow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newDeliveryPool(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	freshID := insertClaimOrderOutboxForDispatch(t, pool, "fresh-within-window", now.Add(-30*time.Minute), now.Add(-1*time.Minute))
	staleID := insertClaimOrderOutboxForDispatch(t, pool, "stale-beyond-window", now.Add(-3*time.Hour), now.Add(-1*time.Minute))

	transition := claimOrderTransitionStore(t, pool)
	rows, err := transition.ClaimOutboxesForFanout(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, freshID, rows[0].ID)
	require.NotEqual(t, staleID, rows[0].ID)
}

func claimOrderTransitionStore(t *testing.T, pool *pgxpool.Pool) *store.TransitionStore {
	t.Helper()

	transition, err := store.NewTransitionStore(pool, nil, store.TransitionConfig{
		MaxRetries: 3, RetryBackoff: time.Minute, LockTimeout: time.Minute,
		ClaimFreshnessWindow: 2 * time.Hour, LogicalGroupLimit: 100,
	})
	require.NoError(t, err)

	return transition
}

func insertClaimOrderOutboxForDispatch(t *testing.T, pool *pgxpool.Pool, contentID string, createdAt, nextAttemptAt time.Time) int64 {
	t.Helper()

	var id int64

	err := pool.QueryRow(t.Context(), `
        INSERT INTO youtube_notification_outbox
            (kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
        VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8)
        RETURNING id
    `, domain.OutboxKindCommunityPost, "UC_dispatch_claim_order", contentID, `{"post_id":"`+contentID+`"}`, domain.OutboxStatusPending, 0, nextAttemptAt, createdAt).Scan(&id)
	require.NoError(t, err)

	return id
}
