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

const (
	requestClockOwner        = "owner"
	requestClockBegin        = "begin"
	requestClockLoad         = "load"
	requestClockLoadUnpinned = "load-unpinned"
	requestClockPin          = "pin"
	requestClockPinExisting  = "pin-existing"
)

func TestSendRequestChecksLeaseAfterAllLocks(t *testing.T) {
	for _, operation := range []string{requestClockBegin, requestClockLoad, requestClockLoadUnpinned, requestClockPin, requestClockPinExisting} {
		for _, lock := range []string{"unit", "last-member"} {
			for _, expired := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/expired=%t", operation, lock, expired), func(t *testing.T) {
					runRequestClockScenario(t, operation, lock, expired)
				})
			}
		}
	}
}

func runRequestClockScenario(t *testing.T, operation, lock string, expired bool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	repo := NewPgxRepositoryFromPool(pool, nil)
	unit, ids := seedRequestClockUnit(t, repo)
	candidate := SendRequest{Body: "frozen body", Route: SendRouteMarkdown}

	var pinned *SendRequest

	if operation != requestClockPin && operation != requestClockLoadUnpinned {
		var err error

		pinned, err = repo.PinSendRequest(t.Context(), unit, ids, requestClockOwner, candidate)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	expires, waitUntil := setRequestClockExpiry(ctx, t, pool, ids[0], expired)

	var loaded *SendRequest

	call := func(callCtx context.Context) error {
		var err error

		switch operation {
		case requestClockBegin:
			return repo.BeginSendRequest(callCtx, unit, ids, requestClockOwner, pinned.ClientRequestID, 7900*time.Millisecond)
		case requestClockLoad, requestClockLoadUnpinned:
			loaded, err = repo.LoadSendRequest(callCtx, unit, ids, requestClockOwner)
		case requestClockPin, requestClockPinExisting:
			loaded, err = repo.PinSendRequest(callCtx, unit, ids, requestClockOwner, candidate)
		default:
			return fmt.Errorf("unsupported request clock operation %q", operation)
		}

		return err
	}

	releasedAt, callErr := runRequestClockBlocked(ctx, t, pool, lock, unit, ids[len(ids)-1], waitUntil, call)
	assertRequestClockOutcome(t, operation, expired, callErr, loaded, &candidate, ids)
	assertRequestClockPersistedState(ctx, t, pool, operation, expired, unit, ids, expires, releasedAt)
}

func setRequestClockExpiry(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id int64, expired bool) (time.Time, time.Time) {
	t.Helper()

	seconds := 60

	if expired {
		seconds = 1
	}

	var expires time.Time

	require.NoError(t, pool.QueryRow(ctx, `UPDATE alarm_dispatch_deliveries
		SET lock_expires_at=clock_timestamp()+($2::int*interval '1 second')
		WHERE id=$1 RETURNING lock_expires_at`, id, seconds).Scan(&expires))

	waitUntil := time.Time{}

	if expired {
		waitUntil = expires
	}

	return expires, waitUntil
}

func assertRequestClockPersistedState(ctx context.Context, t *testing.T, pool *pgxpool.Pool, operation string, expired bool, unit int64, ids []int64, expires, releasedAt time.Time) {
	t.Helper()

	if operation == requestClockBegin && !expired {
		assertRequestClockSending(ctx, t, pool, ids, releasedAt, 7*time.Second)
	} else {
		assertRequestClockLeased(ctx, t, pool, ids)

		var retainedExpiry time.Time

		require.NoError(t, pool.QueryRow(ctx, `SELECT lock_expires_at FROM alarm_dispatch_deliveries WHERE id=$1`, ids[0]).Scan(&retainedExpiry))
		require.True(t, expires.Equal(retainedExpiry))
	}

	if operation == requestClockPin || operation == requestClockLoadUnpinned {
		var hasBody bool

		require.NoError(t, pool.QueryRow(ctx, `SELECT request_body_hash IS NOT NULL FROM alarm_dispatch_send_units WHERE id=$1`, unit).Scan(&hasBody))
		require.Equal(t, operation == requestClockPin && !expired, hasBody)
	}
}

func assertRequestClockOutcome(t *testing.T, operation string, expired bool, callErr error, loaded, candidate *SendRequest, ids []int64) {
	t.Helper()

	if expired {
		require.ErrorIs(t, callErr, ErrSendRequestFence)
		require.Nil(t, loaded)

		return
	}

	if operation == requestClockLoadUnpinned {
		require.ErrorIs(t, callErr, ErrSendRequestUnpinned)

		return
	}

	require.NoError(t, callErr)

	if operation != requestClockBegin {
		require.NotNil(t, loaded)
		require.Equal(t, candidate.Body, loaded.Body)
		require.Equal(t, candidate.Route, loaded.Route)
		require.Equal(t, ids, loaded.DeliveryIDs)
	}
}

func seedRequestClockUnit(t *testing.T, repo *PgxRepository) (int64, []int64) {
	t.Helper()

	first := receiptEnvelope("clock-request-room", "first")
	second := receiptEnvelope("clock-request-room", "second")

	second.Notification.Stream.ID = "clock-second-video"

	_, err := repo.InsertBatch(t.Context(), PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first, second}})
	require.NoError(t, err)

	claimed, err := repo.ClaimDue(t.Context(), requestClockOwner, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	require.Equal(t, claimed[0].SendUnitID, claimed[1].SendUnitID)

	ids := []int64{claimed[0].ID, claimed[1].ID}
	slices.Sort(ids)

	return claimed[0].SendUnitID, ids
}

// 실제 PostgreSQL 대기를 확인한 뒤 잠금을 풀어 transaction/statement 시작 시각과 구별한다.
func runRequestClockBlocked(ctx context.Context, t *testing.T, pool *pgxpool.Pool, lock string, unit, member int64, expires time.Time, call func(context.Context) error) (time.Time, error) {
	t.Helper()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	blocker, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cleanupCancel()

		if rollbackErr := blocker.Rollback(cleanupCtx); !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			require.NoError(t, rollbackErr)
		}
	}()

	if lock == "unit" {
		_, err = blocker.Exec(ctx, `SELECT id FROM alarm_dispatch_send_units WHERE id=$1 FOR UPDATE`, unit)
	} else {
		_, err = blocker.Exec(ctx, `SELECT id FROM alarm_dispatch_deliveries WHERE id=$1 FOR UPDATE`, member)
	}

	require.NoError(t, err)

	done := make(chan error, 1)

	var workers sync.WaitGroup

	workers.Go(func() { done <- call(ctx) })

	defer func() {
		cancel()
		workers.Wait()
	}()

	waitFailureClockLock(ctx, t, pool, blocker.Conn().PgConn().PID(), done)

	if !expires.IsZero() {
		_, err = pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM ($1::timestamptz-clock_timestamp())))::double precision+0.05)`, expires)
		require.NoError(t, err)
	}

	var releasedAt time.Time

	require.NoError(t, blocker.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&releasedAt))
	require.NoError(t, blocker.Commit(ctx))

	return releasedAt, <-done
}

func assertRequestClockLeased(ctx context.Context, t *testing.T, pool *pgxpool.Pool, ids []int64) {
	t.Helper()

	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries
		WHERE id=ANY($1) AND status='leased' AND sending_started_at IS NULL
		AND locked_by=$2 AND attempt_count=0`, ids, requestClockOwner).Scan(&count))
	require.Equal(t, len(ids), count)
}

func assertRequestClockSending(ctx context.Context, t *testing.T, pool *pgxpool.Pool, ids []int64, notBefore time.Time, extension time.Duration) {
	t.Helper()

	var common time.Time

	for _, id := range ids {
		var (
			started, expires, updated time.Time
			status, owner             string
		)

		require.NoError(t, pool.QueryRow(ctx, `SELECT status,locked_by,sending_started_at,lock_expires_at,updated_at
			FROM alarm_dispatch_deliveries WHERE id=$1`, id).Scan(&status, &owner, &started, &expires, &updated))
		require.Equal(t, "sending", status)
		require.Equal(t, requestClockOwner, owner)
		require.False(t, started.Before(notBefore), "sending timestamp must follow the last lock wait")
		require.True(t, started.Equal(updated))
		require.Equal(t, extension, expires.Sub(started))

		if !common.IsZero() {
			require.True(t, common.Equal(started), "all members must share one wall clock")
		}

		common = started
	}
}

func TestMarkSendingPostLockClockPreservesPartialRows(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%t", expired), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			repo := NewPgxRepositoryFromPool(pool, nil)
			unit, ids := seedRequestClockUnit(t, repo)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

			defer cancel()

			var expires time.Time

			if expired {
				require.NoError(t, pool.QueryRow(ctx, `UPDATE alarm_dispatch_deliveries
					SET lock_expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING lock_expires_at`, ids[0]).Scan(&expires))
			}

			releasedAt, err := runRequestClockBlocked(ctx, t, pool, "last-member", unit, ids[1], expires, func(callCtx context.Context) error {
				return repo.MarkSending(callCtx, ids, requestClockOwner, 7900*time.Millisecond)
			})

			if expired {
				require.Error(t, err)
				assertRequestClockLeased(ctx, t, pool, ids[:1])
				assertRequestClockSending(ctx, t, pool, ids[1:], releasedAt, 7*time.Second)
			} else {
				require.NoError(t, err)
				assertRequestClockSending(ctx, t, pool, ids, releasedAt, 7*time.Second)
			}
		})
	}
}

func TestSendingTransitionExpiryBoundaryUsesSuppliedClock(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := NewPgxRepositoryFromPool(pool, nil)
	_, ids := seedRequestClockUnit(t, repo)
	ctx := t.Context()

	var checkedAt time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&checkedAt))

	_, err := pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET lock_expires_at=$2::timestamptz
		+CASE WHEN id=$3 THEN interval '1 microsecond' ELSE interval '0' END WHERE id=ANY($1)`, ids, checkedAt, ids[1])
	require.NoError(t, err)

	tag, err := pool.Exec(ctx, mustSQL("repository_transitions_0020_01.sql"), ids, 9, requestClockOwner, checkedAt)
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected())
	assertRequestClockLeased(ctx, t, pool, ids[:1])
	assertRequestClockSending(ctx, t, pool, ids[1:], checkedAt, 9*time.Second)

	var started time.Time

	require.NoError(t, pool.QueryRow(ctx, `SELECT sending_started_at FROM alarm_dispatch_deliveries WHERE id=$1`, ids[1]).Scan(&started))
	require.True(t, checkedAt.Equal(started), "prepared fence clock must not be sampled again")
}

func TestBeginSendRequestRejectsChangedFencesAtomically(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := NewPgxRepositoryFromPool(pool, nil)
	unit, ids := seedRequestClockUnit(t, repo)
	request, err := repo.PinSendRequest(t.Context(), unit, ids, requestClockOwner, SendRequest{Body: "body", Route: SendRouteText})
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		ids       []int64
		worker    string
		requestID string
	}{
		{name: "owner", ids: ids, worker: "foreign", requestID: request.ClientRequestID},
		{name: "partial-membership", ids: ids[:1], worker: requestClockOwner, requestID: request.ClientRequestID},
		{name: "duplicate-membership", ids: []int64{ids[0], ids[0]}, worker: requestClockOwner, requestID: request.ClientRequestID},
		{name: "empty-membership", worker: requestClockOwner, requestID: request.ClientRequestID},
		{name: "request", ids: ids, worker: requestClockOwner, requestID: request.ClientRequestID + ":wrong"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, repo.BeginSendRequest(t.Context(), unit, tc.ids, tc.worker, tc.requestID, time.Minute), ErrSendRequestFence)
			assertRequestClockLeased(t.Context(), t, pool, ids)
		})
	}

	_, err = pool.Exec(t.Context(), `UPDATE alarm_dispatch_deliveries SET locked_by='foreign' WHERE id=$1`, ids[1])
	require.NoError(t, err)
	require.ErrorIs(t, repo.BeginSendRequest(t.Context(), unit, ids, requestClockOwner, request.ClientRequestID, time.Minute), ErrSendRequestFence)
	assertRequestClockLeased(t.Context(), t, pool, ids[:1])

	var untouched bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT status='leased' AND sending_started_at IS NULL
		AND locked_by='foreign' AND attempt_count=0 FROM alarm_dispatch_deliveries WHERE id=$1`, ids[1]).Scan(&untouched))
	require.True(t, untouched)
}
