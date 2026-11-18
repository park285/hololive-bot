package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	dbtest "github.com/kapu/hololive-dbtest"
)

func TestBeginSendingLeaseStartsAtDatabaseClockAfterLockWait(t *testing.T) {
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-clock-begin", "room-clock-begin")
	freezeTransitionClock(t, pool)

	transition := newTestTransitionStore(t, pool)
	claim := claimAndPrepare(t, pool, transition)

	release := holdRowLock(t, pool, lockDeliveryRowSQL, deliveryID)
	done := make(chan error, 1)

	go func() {
		_, _, err := transition.BeginSending(t.Context(), claim.rows, claim.outboxes)
		done <- err
	}()

	releasedAt := release()

	require.NoError(t, <-done)

	lockedAt := readTransitionTime(t, pool, `SELECT locked_at FROM youtube_notification_delivery WHERE id = $1`, deliveryID)
	require.False(t, lockedAt.Before(releasedAt), "locked_at %s precedes lock release %s", lockedAt, releasedAt)
}

func TestRetryDueCountsFromDatabaseClockAfterLockWait(t *testing.T) {
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-clock-retry", "room-clock-retry")
	freezeTransitionClock(t, pool)

	transition := newTestTransitionStore(t, pool)
	claim := claimAndPrepare(t, pool, transition)

	operation, begin, err := transition.BeginSending(t.Context(), claim.rows, claim.outboxes)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, begin.Outcome)

	const retryAfter = 3 * time.Second

	release := holdRowLock(t, pool, lockDeliveryRowSQL, deliveryID)
	done := make(chan error, 1)

	go func() {
		_, err := transition.ApplyStartedFailure(
			t.Context(), operation, lifecycle.FailureRetryable, lifecycle.Reason("provider_transport"), retryAfter, DeliveryModePerRoom,
		)
		done <- err
	}()

	releasedAt := release()

	require.NoError(t, <-done)

	upper := readTransitionTime(t, pool, `SELECT clock_timestamp()`)
	nextAttemptAt := readTransitionTime(t, pool, `SELECT next_attempt_at FROM youtube_notification_delivery WHERE id = $1`, deliveryID)
	require.False(t, nextAttemptAt.Before(releasedAt.Add(retryAfter)),
		"next_attempt_at %s precedes lock release %s + retry-after", nextAttemptAt, releasedAt)
	require.False(t, nextAttemptAt.After(upper.Add(retryAfter)),
		"next_attempt_at %s exceeds transition end %s + retry-after", nextAttemptAt, upper)
}

func TestFanoutFailureRetryDueCountsFromDatabaseClockAfterLockWait(t *testing.T) {
	pool := dbtest.NewPool(t)
	outboxID := seedTransitionFanoutOutbox(t, pool, "video-clock-fanout-retry")
	freezeTransitionClock(t, pool)

	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimOutboxesForFanout(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	const backoff = time.Second

	release := holdRowLock(t, pool, lockOutboxRowSQL, outboxID)
	done := make(chan error, 1)

	go func() {
		_, err := transition.ApplyFanoutFailure(t.Context(), claimed[0], "subscriber lookup failed")
		done <- err
	}()

	releasedAt := release()

	require.NoError(t, <-done)

	upper := readTransitionTime(t, pool, `SELECT clock_timestamp()`)
	nextAttemptAt := readTransitionTime(t, pool, `SELECT next_attempt_at FROM youtube_notification_outbox WHERE id = $1`, outboxID)
	require.False(t, nextAttemptAt.Before(releasedAt.Add(backoff)),
		"next_attempt_at %s precedes lock release %s + backoff", nextAttemptAt, releasedAt)
	require.False(t, nextAttemptAt.After(upper.Add(backoff)),
		"next_attempt_at %s exceeds transition end %s + backoff", nextAttemptAt, upper)
}

func TestFollowerDeferDueRespectsOwnerFloor(t *testing.T) {
	t.Run("owner due later than backoff", func(t *testing.T) {
		pool := dbtest.NewPool(t)
		ownerID, followerID := seedTransitionLogicalGroup(t, pool, "video-clock-floor-late", "room-clock-floor-late")
		transition := newTestTransitionStore(t, pool)

		_, err := pool.Exec(t.Context(), `
			UPDATE youtube_notification_delivery SET next_attempt_at = clock_timestamp() + INTERVAL '1 hour' WHERE id = $1
		`, ownerID)
		require.NoError(t, err)

		ownerDue := readTransitionTime(t, pool, `SELECT next_attempt_at FROM youtube_notification_delivery WHERE id = $1`, ownerID)

		claimed, err := transition.ClaimPending(t.Context(), 10)
		require.NoError(t, err)
		require.Len(t, claimed, 1)
		require.Equal(t, followerID, claimed[0].ID)

		prepared, err := transition.PrepareClaimed(t.Context(), claimed, loadTransitionTestOutboxes(t, pool, claimed))
		require.NoError(t, err)
		require.Empty(t, prepared.ActiveRows)

		followerDue := readTransitionTime(t, pool, `SELECT next_attempt_at FROM youtube_notification_delivery WHERE id = $1`, followerID)
		require.True(t, followerDue.Equal(ownerDue), "follower due %s must take the owner due %s", followerDue, ownerDue)
	})

	t.Run("owner due already passed", func(t *testing.T) {
		pool := dbtest.NewPool(t)
		_, followerID := seedTransitionLogicalGroup(t, pool, "video-clock-floor-past", "room-clock-floor-past")
		transition := newTestTransitionStore(t, pool)

		claimed, err := transition.ClaimPending(t.Context(), 10)
		require.NoError(t, err)
		require.Len(t, claimed, 2)

		before := readTransitionTime(t, pool, `SELECT clock_timestamp()`)

		prepared, err := transition.PrepareClaimed(t.Context(), claimed, loadTransitionTestOutboxes(t, pool, claimed))
		require.NoError(t, err)
		require.Len(t, prepared.ActiveRows, 1)

		after := readTransitionTime(t, pool, `SELECT clock_timestamp()`)
		followerDue := readTransitionTime(t, pool, `SELECT next_attempt_at FROM youtube_notification_delivery WHERE id = $1`, followerID)
		require.False(t, followerDue.Before(before.Add(time.Second)),
			"follower due %s must be at least the database clock %s + backoff", followerDue, before)
		require.False(t, followerDue.After(after.Add(time.Second)),
			"follower due %s must not exceed the database clock %s + backoff", followerDue, after)
	})
}

func TestClaimPendingLeaseUsesDatabaseClock(t *testing.T) {
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-clock-claim", "room-clock-claim")
	transition := newTestTransitionStore(t, pool)

	freezeTransitionClock(t, pool)
	setTransitionLockAge(t, pool, deliveryID, time.Minute-5*time.Second)

	claimed, err := transition.ClaimPending(t.Context(), 10)
	require.NoError(t, err)
	require.Empty(t, claimed, "a lease younger than LockTimeout must not be re-claimed")

	setTransitionLockAge(t, pool, deliveryID, time.Minute+time.Second)

	before := readTransitionTime(t, pool, `SELECT clock_timestamp()`)

	claimed, err = transition.ClaimPending(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NotNil(t, claimed[0].LockedAt)
	require.False(t, claimed[0].LockedAt.Before(before), "new lease must start at the database clock")
}

func TestStaleSendingQuarantineUsesDatabaseClock(t *testing.T) {
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-clock-stale", "room-clock-stale")
	freezeTransitionClock(t, pool)

	transition := newTestTransitionStore(t, pool)
	claim := claimAndPrepare(t, pool, transition)

	_, begin, err := transition.BeginSending(t.Context(), claim.rows, claim.outboxes)
	require.NoError(t, err)
	require.Equal(t, ApplyApplied, begin.Outcome)

	setTransitionLockAge(t, pool, deliveryID, time.Minute-5*time.Second)

	result, err := transition.QuarantineStaleLogicalGroups(t.Context(), 10)
	require.NoError(t, err)
	require.Zero(t, result.QuarantinedDeliveries, "a live sending lease must not be quarantined")

	setTransitionLockAge(t, pool, deliveryID, time.Minute+time.Second)

	result, err = transition.QuarantineStaleLogicalGroups(t.Context(), 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.QuarantinedDeliveries)
	assertTransitionClaimRow(t, pool, deliveryID, DeliveryStatusQuarantined, claim.rows[0].RowVersion+2)
}

const (
	lockDeliveryRowSQL = `SELECT id FROM youtube_notification_delivery WHERE id = $1 FOR UPDATE`
	lockOutboxRowSQL   = `SELECT id FROM youtube_notification_outbox WHERE id = $1 FOR UPDATE`
)

func freezeTransitionClock(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		CREATE TABLE transition_clock AS SELECT clock_timestamp() AS at;
		CREATE FUNCTION public.clock_timestamp() RETURNS timestamptz LANGUAGE sql VOLATILE
			AS 'SELECT at FROM public.transition_clock';
		DO $$ BEGIN
			EXECUTE format('ALTER DATABASE %I SET search_path TO public, pg_catalog', current_database());
		END $$`)
	require.NoError(t, err)
	pool.Reset()
}

func holdRowLock(t *testing.T, pool *pgxpool.Pool, lockQuery string, id int64) func() time.Time {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() { rollbackPlanModeSQLTest(t, tx) })

	_, err = tx.Exec(t.Context(), lockQuery, id)
	require.NoError(t, err)

	return func() time.Time {
		t.Helper()

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		for {
			var waiting bool

			err := pool.QueryRow(ctx, `
				SELECT EXISTS (SELECT FROM pg_stat_activity WHERE $1::int = ANY(pg_blocking_pids(pid)))
			`, tx.Conn().PgConn().PID()).Scan(&waiting)
			require.NoError(t, err, "transition never waited on the row lock")

			if waiting {
				break
			}
		}

		var releasedAt time.Time

		require.NoError(t, tx.QueryRow(t.Context(), `
			UPDATE transition_clock SET at = at + INTERVAL '1 second' RETURNING at
		`).Scan(&releasedAt))
		require.NoError(t, tx.Commit(t.Context()))

		return releasedAt
	}
}

func setTransitionLockAge(t *testing.T, pool *pgxpool.Pool, deliveryID int64, age time.Duration) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		UPDATE youtube_notification_delivery
		SET locked_at = clock_timestamp() - $2::bigint * INTERVAL '1 millisecond'
		WHERE id = $1
	`, deliveryID, age.Milliseconds())
	require.NoError(t, err)
}

func readTransitionTime(t *testing.T, pool *pgxpool.Pool, query string, args ...any) time.Time {
	t.Helper()

	var value time.Time

	require.NoError(t, pool.QueryRow(t.Context(), query, args...).Scan(&value))

	return value
}
