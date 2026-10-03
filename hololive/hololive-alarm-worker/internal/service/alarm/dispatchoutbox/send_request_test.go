package dispatchoutbox

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestSendRequestPinsWholeUnitAndReissuesDurably(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := NewPgxRepositoryFromPool(pool, nil)
	ctx := t.Context()
	first := receiptEnvelope("request-room", "first")
	second := receiptEnvelope("request-room", "second")

	second.Notification.Stream.ID = "second-video"

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first, second}})
	require.NoError(t, err)

	records, err := repository.ClaimDue(ctx, "owner", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 2)

	ids := []int64{records[0].ID, records[1].ID}
	unit := records[0].SendUnitID

	_, err = repository.PinSendRequest(ctx, unit, ids[:1], "owner", SendRequest{Body: "partial", Route: SendRouteText})
	require.ErrorIs(t, err, ErrSendRequestFence)

	_, err = repository.PinSendRequest(ctx, unit, ids, "foreign", SendRequest{Body: "foreign", Route: SendRouteText})
	require.ErrorIs(t, err, ErrSendRequestFence)

	request := pinConcurrentRequest(t, repository, unit, ids)

	for generation := range 3 {
		require.Equal(t, generation, request.Generation)
		require.NoError(t, repository.BeginSendRequest(ctx, unit, ids, "owner", request.ClientRequestID, time.Minute))

		updates := make([]FailureUpdate, len(ids))
		for i, id := range ids {
			updates[i] = FailureUpdate{ID: id, AttemptCount: generation + 1, NextAttemptAt: time.Now().Add(-time.Second), TargetStatus: StatusRetry}
		}

		reissued, err := repository.ReissueSendRequest(ctx, unit, ids, "owner", request.ClientRequestID, updates)
		require.NoError(t, err)

		if generation == 2 {
			require.False(t, reissued)

			break
		}

		require.True(t, reissued)

		published, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first, second}})
		require.NoError(t, err)
		require.Equal(t, 2, published.DuplicateDeliveries)

		_, err = repository.ReissueSendRequest(ctx, unit, ids, "owner", request.ClientRequestID, updates)
		require.ErrorIs(t, err, ErrSendRequestFence)

		repository = NewPgxRepositoryFromPool(pool, nil)
		request = assertPinnedReplay(t, repository, unit, ids, request, generation+1)
	}
}

func TestSendRequestRejectsLegacyAttemptAndPartialMembership(t *testing.T) {
	for _, attempted := range []bool{false, true} {
		t.Run(fmt.Sprint(attempted), func(t *testing.T) {
			pool := dbtest.NewPool(t)
			repository := NewPgxRepositoryFromPool(pool, nil)
			ctx := t.Context()
			_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("legacy-room", "legacy")}})
			require.NoError(t, err)

			if attempted {
				_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET attempt_count=1, sending_started_at=NOW()`)
				require.NoError(t, err)
			}

			records, err := repository.ClaimDue(ctx, "owner", 10, time.Minute)
			require.NoError(t, err)
			require.Len(t, records, 1)

			ids := []int64{records[0].ID}
			unit := records[0].SendUnitID
			require.ErrorIs(t, repository.BeginSendRequest(ctx, unit, ids, "owner", records[0].ClientRequestID, time.Minute), ErrSendRequestFence)

			request, err := repository.PinSendRequest(ctx, unit, ids, "owner", SendRequest{Body: "new", Route: SendRouteText})

			if attempted {
				require.ErrorIs(t, err, ErrLegacySendRequest)
				require.Nil(t, request)
			} else {
				require.NoError(t, err)
				require.NotNil(t, request)
			}
		})
	}
}

func TestSendRequestCanPinAfterKnownUnsentFailure(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := NewPgxRepositoryFromPool(pool, nil)
	ctx := t.Context()
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("pre-render-room", "title")}})
	require.NoError(t, err)

	records, err := repository.ClaimDue(ctx, "owner", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 1)

	ids := []int64{records[0].ID}
	unit := records[0].SendUnitID
	request, err := repository.LoadSendRequest(ctx, unit, ids, "owner")
	require.ErrorIs(t, err, ErrSendRequestUnpinned)
	require.Nil(t, request)
	require.NoError(t, repository.RouteFailures(ctx, []FailureUpdate{{ID: ids[0], AttemptCount: 1, NextAttemptAt: time.Now().Add(-time.Second), TargetStatus: StatusRetry, Error: "template unavailable"}}, "owner"))

	records, err = repository.ClaimDue(ctx, "owner", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 1)

	request, err = repository.PinSendRequest(ctx, unit, ids, "owner", SendRequest{Body: "recovered template", Route: SendRouteText})
	require.NoError(t, err)
	require.NotNil(t, request)
	require.NoError(t, repository.BeginSendRequest(ctx, unit, ids, "owner", request.ClientRequestID, time.Minute))
}

func pinConcurrentRequest(t *testing.T, repository *PgxRepository, unit int64, ids []int64) *SendRequest {
	t.Helper()

	ctx := t.Context()
	// 동시 pin의 승자 하나가 이후 모든 request를 결정한다.
	results := make([]*SendRequest, 2)
	errs := make([]error, 2)

	var wg sync.WaitGroup

	for i := range 2 {
		wg.Go(func() {
			results[i], errs[i] = repository.PinSendRequest(ctx, unit, ids, "owner", SendRequest{Body: fmt.Sprintf("body-%d", i), Route: SendRouteMarkdown})
		})
	}

	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, results[0], results[1])

	require.NotNil(t, results[0])

	return results[0]
}

func assertPinnedReplay(t *testing.T, repository *PgxRepository, unit int64, ids []int64, request *SendRequest, generation int) *SendRequest {
	t.Helper()

	ctx := t.Context()
	// 저장 직후 프로세스 종료를 새 repository·claim으로 대체한다.
	records, err := repository.ClaimDue(ctx, "owner", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 2)

	replay, err := repository.PinSendRequest(ctx, unit, ids, "owner", SendRequest{Body: "changed template", Route: SendRouteText})
	require.NoError(t, err)
	require.NotNil(t, replay)
	require.Equal(t, request.Body, replay.Body)
	require.Equal(t, request.Route, replay.Route)
	require.Equal(t, request.BodyHash, replay.BodyHash)
	require.Equal(t, request.BaseClientRequestID+fmt.Sprintf(":r%d", generation), replay.ClientRequestID)

	return replay
}

func TestReissueRequestRollsBackWholeUnitWhenMemberAttemptFenceFails(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := NewPgxRepositoryFromPool(pool, nil)
	ctx := t.Context()
	first := receiptEnvelope("rollback-request", "one")
	second := receiptEnvelope("rollback-request", "two")

	second.Notification.Stream.ID = "rollback-second"

	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first, second}})
	require.NoError(t, err)

	records, err := repository.ClaimDue(ctx, "owner", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 2)

	ids := []int64{records[0].ID, records[1].ID}
	unit := records[0].SendUnitID
	request, err := repository.PinSendRequest(ctx, unit, ids, "owner", SendRequest{Body: "body", Route: SendRouteText})
	require.NoError(t, err)
	require.NotNil(t, request)
	require.NoError(t, repository.BeginSendRequest(ctx, unit, ids, "owner", request.ClientRequestID, time.Minute))

	updates := []FailureUpdate{
		{ID: ids[0], AttemptCount: 1, TargetStatus: StatusRetry, NextAttemptAt: time.Now()},
		{ID: ids[1], AttemptCount: 99, TargetStatus: StatusRetry, NextAttemptAt: time.Now()},
	}
	reissued, err := repository.ReissueSendRequest(ctx, unit, ids, "owner", request.ClientRequestID, updates)
	require.ErrorIs(t, err, ErrSendRequestFence)
	require.False(t, reissued)

	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries WHERE send_unit_id=$1 AND status='sending' AND attempt_count=0 AND locked_by='owner'`, unit).Scan(&count))
	require.Equal(t, 2, count)

	var generation int

	require.NoError(t, pool.QueryRow(ctx, `SELECT request_generation FROM alarm_dispatch_send_units WHERE id=$1`, unit).Scan(&generation))
	require.Zero(t, generation)
}
