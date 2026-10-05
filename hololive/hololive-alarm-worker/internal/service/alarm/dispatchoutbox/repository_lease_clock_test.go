package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRouteFailuresChecksExpiryAfterAllRowLocks(t *testing.T) {
	for _, target := range []Status{StatusRetry, StatusDLQ} {
		for _, size := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/%d", target, size), func(t *testing.T) {
				pool := dbtest.NewPool(t)
				repo := NewPgxRepositoryFromPool(pool, nil)
				ids := seedFailureClockRows(t, repo, size)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

				defer cancel()

				var expires time.Time

				require.NoError(t, pool.QueryRow(ctx, `UPDATE alarm_dispatch_deliveries
					SET lock_expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING lock_expires_at`, ids[0]).Scan(&expires))

				blocker, err := pool.Begin(ctx)
				require.NoError(t, err)

				defer func() {
					if rollbackErr := blocker.Rollback(t.Context()); !errors.Is(rollbackErr, pgx.ErrTxClosed) {
						require.NoError(t, rollbackErr)
					}
				}()

				// 두 행일 때는 앞 행의 잠금을 얻고 뒤 행을 기다리는 동안 앞 lease만 만료된다.
				_, err = blocker.Exec(ctx, "SELECT id FROM alarm_dispatch_deliveries WHERE id=$1 FOR UPDATE", ids[len(ids)-1])
				require.NoError(t, err)

				updates := make([]FailureUpdate, len(ids))
				for i, id := range ids {
					updates[i] = FailureUpdate{ID: id, AttemptCount: 1, TargetStatus: target, NextAttemptAt: time.Now().UTC().Add(time.Minute)}
				}

				done := make(chan error, 1)

				var workers sync.WaitGroup

				workers.Go(func() { done <- repo.RouteFailures(ctx, updates, "clock-worker") })

				defer func() {
					cancel()
					workers.Wait()
				}()

				waitFailureClockLock(ctx, t, pool, blocker.Conn().PgConn().PID(), done)

				_, err = pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp())))::double precision+0.05)`, expires)
				require.NoError(t, err)
				require.NoError(t, blocker.Commit(ctx))

				var partial *PartialTransitionError

				require.ErrorAs(t, <-done, &partial)
				require.Equal(t, int64(size-1), partial.Updated)
				require.Equal(t, []int64{ids[0]}, partial.UnappliedIDs)
				assertFailureClockRows(ctx, t, pool, ids, target)
			})
		}
	}
}

func seedFailureClockRows(t *testing.T, repo *PgxRepository, count int) []int64 {
	t.Helper()

	envelopes := make([]domain.AlarmQueueEnvelope, count)
	for i := range count {
		envelopes[i] = domain.AlarmQueueEnvelope{Notification: *candidateNotification(fmt.Sprintf("clock-room-%d", i), time.Now().UTC().Truncate(time.Minute)), Version: 1}
	}

	_, err := repo.InsertBatch(t.Context(), PublishBatchInput{Envelopes: envelopes})
	require.NoError(t, err)

	claimed, err := repo.ClaimDue(t.Context(), "clock-worker", count, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, count)

	ids := make([]int64, len(claimed))
	for i := range claimed {
		ids[i] = claimed[i].ID
	}

	slices.Sort(ids)

	return ids
}

func waitFailureClockLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, blockerPID uint32, done <-chan error) {
	t.Helper()

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		var blocked bool

		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&blocked))

		if blocked {
			return
		}

		select {
		case callErr := <-done:
			t.Fatalf("failure routing did not wait on row lock: %v", callErr)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertFailureClockRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool, ids []int64, target Status) {
	t.Helper()

	for i, id := range ids {
		var status string

		var attempts int

		var owner *string

		require.NoError(t, pool.QueryRow(ctx, "SELECT status,attempt_count,locked_by FROM alarm_dispatch_deliveries WHERE id=$1", id).Scan(&status, &attempts, &owner))

		if i == 0 {
			require.Equal(t, string(StatusLeased), status)
			require.Zero(t, attempts)
			require.Equal(t, new("clock-worker"), owner)
		} else {
			require.Equal(t, string(target), status)
			require.Equal(t, 1, attempts)
			require.Nil(t, owner)
		}
	}
}
