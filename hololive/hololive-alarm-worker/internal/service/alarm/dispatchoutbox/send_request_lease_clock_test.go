package dispatchoutbox

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
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
	repo := newTestRepository(t, pool)
	unit, ids, requestID := seedRequestClockUnit(t, repo)
	claims := attemptClaims(0, ids...)
	candidate := SendRequest{Body: "frozen body", Route: SendRouteRichText}

	var pinned *SendRequest

	if operation != requestClockPin && operation != requestClockLoadUnpinned {
		var err error

		pinned, err = repo.PinSendRequest(t.Context(), unit, claims, requestClockOwner, requestID, candidate)
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
			return repo.BeginSendRequest(callCtx, unit, claims, requestClockOwner, pinned.ClientRequestID, 7900*time.Millisecond)
		case requestClockLoad, requestClockLoadUnpinned:
			loaded, err = repo.LoadSendRequest(callCtx, unit, claims, requestClockOwner, requestID)
		case requestClockPin, requestClockPinExisting:
			loaded, err = repo.PinSendRequest(callCtx, unit, claims, requestClockOwner, requestID, candidate)
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
		assertRequestClockSending(ctx, t, pool, ids, releasedAt, 7900*time.Millisecond)
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
		miss := requireFenceMiss(t, callErr, sendfence.ReasonLeaseExpired)
		require.Zero(t, miss.Applied)
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

// seedRequestClockUnit은 두 멤버 send unit을 claim하고 unit ID, 정렬한 멤버 ID, claim에서 본 요청 ID를 돌려준다.
func seedRequestClockUnit(t *testing.T, repo *PgxRepository) (int64, []int64, string) {
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

	return claimed[0].SendUnitID, ids, claimed[0].ClientRequestID
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

		if rollbackErr := pgxdb.Rollback(cleanupCtx, blocker); rollbackErr != nil {
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

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(id) FROM alarm_dispatch_deliveries
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

// BeginSendRequest는 한 문장에서 단위 전원의 fence를 확인하므로, 어느 조건이 어긋나도 아무 멤버도 바꾸지 않고
// 대상마다 원인을 돌려준다.
func TestBeginSendRequestRejectsChangedFencesAtomically(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := newTestRepository(t, pool)
	unit, ids, requestID := seedRequestClockUnit(t, repo)
	claims := attemptClaims(0, ids...)
	request, err := repo.PinSendRequest(t.Context(), unit, claims, requestClockOwner, requestID, SendRequest{Body: "body", Route: SendRouteText})
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		claims    []DeliveryClaim
		worker    string
		requestID string
		reason    sendfence.Reason
	}{
		{name: "owner", claims: claims, worker: "foreign", requestID: request.ClientRequestID, reason: sendfence.ReasonOwnerChanged},
		{name: "stale-attempt", claims: attemptClaims(1, ids...), worker: requestClockOwner, requestID: request.ClientRequestID, reason: sendfence.ReasonVersionChanged},
		{name: "partial-membership", claims: claims[:1], worker: requestClockOwner, requestID: request.ClientRequestID, reason: sendfence.ReasonMembershipChanged},
		{name: "request", claims: claims, worker: requestClockOwner, requestID: request.ClientRequestID + ":wrong", reason: sendfence.ReasonRequestChanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miss := requireFenceMiss(t, repo.BeginSendRequest(t.Context(), unit, tc.claims, tc.worker, tc.requestID, time.Minute), tc.reason)
			require.Zero(t, miss.Applied)
			assertRequestClockLeased(t.Context(), t, pool, ids)
		})
	}

	require.Error(t, repo.BeginSendRequest(t.Context(), unit, nil, requestClockOwner, request.ClientRequestID, time.Minute))
	assertRequestClockLeased(t.Context(), t, pool, ids)

	// 멤버 하나가 다른 소유자에게 넘어가면 그 멤버는 owner_changed, 나머지는 함께 막힌 peer_missed다.
	_, err = pool.Exec(t.Context(), `UPDATE alarm_dispatch_deliveries SET locked_by='foreign' WHERE id=$1`, ids[1])
	require.NoError(t, err)

	miss := requireFenceMiss(t, repo.BeginSendRequest(t.Context(), unit, claims, requestClockOwner, request.ClientRequestID, time.Minute), sendfence.ReasonOwnerChanged)
	require.Equal(t, []sendfence.RowMiss{
		{ID: ids[0], Reason: sendfence.ReasonPeerMissed, Status: string(StatusLeased)},
		{ID: ids[1], Reason: sendfence.ReasonOwnerChanged, Status: string(StatusLeased)},
	}, miss.Rows)
	assertRequestClockLeased(t.Context(), t, pool, ids[:1])

	var untouched bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT status='leased' AND sending_started_at IS NULL
		AND locked_by='foreign' AND attempt_count=0 FROM alarm_dispatch_deliveries WHERE id=$1`, ids[1]).Scan(&untouched))
	require.True(t, untouched)
}
