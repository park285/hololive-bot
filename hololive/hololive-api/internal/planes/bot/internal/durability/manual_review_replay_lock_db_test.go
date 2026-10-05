package durability

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestManualReviewReplayChecksCutoffAfterLockWait(t *testing.T) {
	pool := newDurabilityPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)

	defer cancel()

	repo := NewReplyOutboxRepository(pool)
	entry := newReplyOutboxEntry("message:replay-lock-cutoff", 0, `{"body":"stored"}`)
	_, err := repo.Insert(ctx, entry)
	require.NoError(t, err)

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	pid := conn.Conn().PgConn().PID()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, rollbackInboxTx(ctx, tx)) }()

	var (
		id     int64
		cutoff time.Time
	)

	err = tx.QueryRow(ctx, `UPDATE bot_reply_outbox
		SET status='manual_review', attempts=5, first_attempt_at=clock_timestamp()-interval '1 hour',
		    created_at=clock_timestamp()-interval '144 hours'+interval '2 seconds'
		WHERE message_id=$1 RETURNING id,created_at+interval '144 hours'`, entry.MessageID).Scan(&id, &cutoff)
	require.NoError(t, err)

	type replayResult struct {
		outcome string
		err     error
	}

	done := make(chan replayResult, 1)

	go func() {
		var result replayResult

		result.err = conn.QueryRow(ctx, `SELECT grant_bot_reply_outbox_manual_replay($1,$2,$3)`,
			id, testOperatorEmail, manualReplayReason).Scan(&result.outcome)

		done <- result
	}()

	waitThroughManualReplayCutoff(ctx, t, pool, pid, cutoff)
	require.NoError(t, tx.Commit(ctx))

	select {
	case result := <-done:
		require.NoError(t, result.err)
		require.Equal(t, "cutoff_expired", result.outcome)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	var auditCount int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM bot_reply_outbox_replay_audit WHERE outbox_id=$1`, id).Scan(&auditCount))
	require.Zero(t, auditCount)
	assertRejectedReplayStaysUnclaimable(ctx, t, pool, repo, id)
}

func waitThroughManualReplayCutoff(ctx context.Context, t *testing.T, pool *pgxpool.Pool, pid uint32, cutoff time.Time) {
	t.Helper()

	var blockedAt time.Time

	require.Eventually(t, func() bool {
		var blocked bool

		lockErr := pool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1::integer))>0,clock_timestamp()`, pid).
			Scan(&blocked, &blockedAt)

		return lockErr == nil && blocked
	}, time.Second, 10*time.Millisecond)
	require.True(t, blockedAt.Before(cutoff), "call must enter the lock wait before the cutoff")

	// 호스트 시계와 무관하게 DB의 cutoff가 지난 후에만 행 잠금을 해제합니다.
	_, err := pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.05)`, cutoff)
	require.NoError(t, err)
}
