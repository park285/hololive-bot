package joblease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/pgxutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func TestDeferMinimumStartsAfterRowLockWait(t *testing.T) {
	assertLeaseTransitionAfterRowLock(t, false, "")
}

func TestDeferRejectsLeaseExpiredDuringLockWait(t *testing.T) {
	assertLeaseTransitionAfterRowLock(t, true, "")
}

func TestReleaseRejectsLeaseExpiredDuringLockWait(t *testing.T) {
	for _, reason := range []ReleaseReason{ReleaseShutdown, ReleaseRenewFail, ReleaseSuperseded} {
		t.Run(string(reason), func(t *testing.T) { assertLeaseTransitionAfterRowLock(t, true, reason) })
	}
}

func TestReleaseJitterStartsAfterRowLockWait(t *testing.T) {
	assertLeaseTransitionAfterRowLock(t, false, ReleaseShutdown)
}

func assertLeaseTransitionAfterRowLock(t *testing.T, expireDuringWait bool, reason ReleaseReason) {
	t.Helper()

	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})

	lease := mustAcquireLease(t, newTestRepository(t, pool), communityJob(), "collector-a")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	defer cancel()

	lifetime := int64(60000)
	waitSeconds := 0.2

	if expireDuringWait {
		lifetime, waitSeconds = 1000, 1.1
	}

	_, err := pool.Exec(ctx, `UPDATE youtube_collection_job_leases SET lease_expires_at=clock_timestamp()+($2::bigint*interval '1 millisecond') WHERE job_key=$1`, lease.proof.JobKey, lifetime)
	require.NoError(t, err)

	locker, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackTestLease(ctx, t, locker)

	_, err = locker.Exec(ctx, `SELECT job_key FROM youtube_collection_job_leases WHERE job_key=$1 FOR UPDATE`, lease.proof.JobKey)
	require.NoError(t, err)

	blockerPID := locker.Conn().PgConn().PID()
	input := retryDeadlineInput(t, retryDatabaseClock(t, pool).Add(-time.Hour), false)
	done := make(chan error, 1)

	go func() {
		if reason != "" {
			done <- lease.Release(ctx, reason)
			return
		}

		done <- lease.Defer(ctx, input)
	}()

	waitForLeaseQueryLock(ctx, t, pool, blockerPID)

	_, err = pool.Exec(ctx, `SELECT pg_sleep($1::double precision)`, waitSeconds)
	require.NoError(t, err)

	releasedAt := retryDatabaseClock(t, pool)
	require.NoError(t, locker.Commit(ctx))

	select {
	case err := <-done:
		if expireDuringWait {
			require.ErrorIs(t, err, collection.ErrFenceLost)

			var state string

			require.NoError(t, pool.QueryRow(ctx, `SELECT slot_state FROM youtube_collection_job_leases WHERE job_key=$1`, lease.proof.JobKey).Scan(&state))
			require.Equal(t, "ACTIVE", state)

			return
		}

		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	assertRetryDelayAfterLock(ctx, t, pool, lease, releasedAt, reason)
}

func waitForLeaseQueryLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, blockerPID uint32) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool

		err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&waiting)

		return err == nil && waiting
	}, time.Second, 10*time.Millisecond)
}

func rollbackTestLease(ctx context.Context, t *testing.T, tx pgxutil.Rollbacker) {
	t.Helper()

	if err := pgxutil.Rollback(ctx, tx); !errors.Is(err, pgx.ErrTxClosed) {
		require.NoError(t, err)
	}
}

func assertRetryDelayAfterLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, lease *JobLease, releasedAt time.Time, reason ReleaseReason) {
	t.Helper()

	minimum := testRetryBounds.Minimum

	var stored time.Time

	if reason != "" {
		minimum = lease.repository.config.MinReleaseJitter
		require.NoError(t, pool.QueryRow(ctx, `SELECT retry_not_before FROM youtube_collection_job_leases WHERE job_key=$1`, lease.proof.JobKey).Scan(&stored))
	} else {
		stored = readRetryDeadlineWithContract(ctx, t, pool, lease.proof)
	}

	require.False(t, stored.Before(releasedAt.Add(minimum)), "lock waiting must not consume the minimum retry delay")
}
