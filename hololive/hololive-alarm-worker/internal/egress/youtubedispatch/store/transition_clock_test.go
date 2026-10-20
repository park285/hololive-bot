package store

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	dbtest "github.com/kapu/hololive-dbtest"
)

// 발송 시작 lease는 호출자가 시각을 잰 때가 아니라 전이가 행을 잠근 뒤의 DB 시계에서 시작해야 한다.
// 잠금 대기가 길어도 새 locked_at이 대기 이전 시각으로 남아 lease가 일찍 끝나면 안 된다.
func TestBeginSendingLeaseStartsAtDatabaseClockAfterLockWait(t *testing.T) {
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-clock-begin", "room-clock-begin")
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

// 발송 뒤 재시도 시각은 전이 시점 DB 시계 + max(backoff, retryAfter)다. 잠금 대기 앞에서 잰 시각을 쓰면 재시도가
// 대기만큼 앞당겨진다.
func TestRetryDueCountsFromDatabaseClockAfterLockWait(t *testing.T) {
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-clock-retry", "room-clock-retry")
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

// fanout 실패의 재시도 시각은 outbox 행을 잠근 뒤의 DB 시계 + RetryBackoff × 다음 시도 횟수다. 잠금 대기 앞에서
// 잰 시각을 쓰면 재시도가 대기만큼 앞당겨진다.
func TestFanoutFailureRetryDueCountsFromDatabaseClockAfterLockWait(t *testing.T) {
	pool := dbtest.NewPool(t)
	outboxID := seedTransitionFanoutOutbox(t, pool, "video-clock-fanout-retry")
	transition := newTestTransitionStore(t, pool)

	claimed, err := transition.ClaimOutboxesForFanout(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	// 첫 실패의 다음 시도 횟수는 1이므로 지연은 RetryBackoff(1초) 한 번이다.
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

// follower 보류 시각은 GREATEST(DB 시계 + RetryBackoff, owner의 next_attempt_at)이다. 소유자의 다음 시도 시각이 더
// 늦으면 그 값을, 이미 지났으면 DB 시계 + backoff를 쓴다.
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

// claim lease 만료는 DB에 기록된 locked_at과 DB 시계로만 판정한다.
func TestClaimPendingLeaseUsesDatabaseClock(t *testing.T) {
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-clock-claim", "room-clock-claim")
	transition := newTestTransitionStore(t, pool)

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

// stale SENDING 격리는 DB의 발송 시작 시각과 DB 시계로 lease 만료를 판정한다.
func TestStaleSendingQuarantineUsesDatabaseClock(t *testing.T) {
	pool := dbtest.NewPool(t)
	deliveryID := seedTransitionSingleDelivery(t, pool, "video-clock-stale", "room-clock-stale")
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

// holdRowLock은 다른 트랜잭션이 lockQuery로 행을 잠근 상태를 만든다. 반환한 release는 전이가 그 잠금에서
// 기다리기 시작한 것을 확인하고 잠시 더 머문 뒤 잠금을 풀며, 푼 직전의 DB 시계를 돌려준다.
func holdRowLock(t *testing.T, pool *pgxpool.Pool, lockQuery string, id int64) func() time.Time {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() { rollbackPlanModeSQLTest(t, tx) })

	_, err = tx.Exec(t.Context(), lockQuery, id)
	require.NoError(t, err)

	return func() time.Time {
		t.Helper()

		require.Eventually(t, func() bool {
			var waiting int64

			err := pool.QueryRow(t.Context(), `
				SELECT count(pid) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()
			`).Scan(&waiting)

			return err == nil && waiting > 0
		}, 10*time.Second, 20*time.Millisecond, "transition never waited on the row lock")

		time.Sleep(300 * time.Millisecond)

		var releasedAt time.Time

		require.NoError(t, tx.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&releasedAt))
		require.NoError(t, tx.Commit(t.Context()))

		return releasedAt
	}
}

// setTransitionLockAge는 버전을 바꾸지 않고 locked_at을 DB 시계 기준 age 전으로 옮긴다.
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
