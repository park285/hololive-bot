package dispatchoutbox

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestSendRequestPinsWholeUnitAndReissuesDurably(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := newTestRepository(t, pool)
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
	claims := recordClaims(records...)
	requestID := records[0].ClientRequestID

	_, err = repository.PinSendRequest(ctx, unit, claims[:1], "owner", requestID, SendRequest{Body: "partial", Route: SendRouteText})
	require.Zero(t, requireFenceMiss(t, err, sendfence.ReasonMembershipChanged).Applied)

	_, err = repository.PinSendRequest(ctx, unit, claims, "foreign", requestID, SendRequest{Body: "foreign", Route: SendRouteText})
	require.Zero(t, requireFenceMiss(t, err, sendfence.ReasonOwnerChanged).Applied)

	request := pinConcurrentRequest(t, repository, unit, claims, requestID)

	for generation := range 3 {
		require.Equal(t, generation, request.Generation)
		require.NoError(t, repository.BeginSendRequest(ctx, unit, attemptClaims(generation, ids...), "owner", request.ClientRequestID, time.Minute))

		// 확정 미발송의 sending 보상은 lease 만료 후에도 소유권으로 허용한다.
		_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET lock_expires_at=clock_timestamp()-interval '1 second' WHERE id=ANY($1)`, ids)
		require.NoError(t, err)

		updates := make([]FailureUpdate, len(ids))
		for i, id := range ids {
			updates[i] = FailureUpdate{ID: id, AttemptCount: generation + 1, TargetStatus: StatusRetry}
		}

		err = repository.ReissueSendRequest(ctx, unit, "owner", request, updates)

		if generation == 2 {
			require.ErrorIs(t, err, sendfence.ErrGenerationExhausted)

			break
		}

		require.NoError(t, err)

		published, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first, second}})
		require.NoError(t, err)
		require.Equal(t, 2, published.DuplicateDeliveries)

		// 같은 세대 스냅샷으로 다시 재발급하면 이미 retry로 정산된 행을 건드리지 않는다.
		err = repository.ReissueSendRequest(ctx, unit, "owner", request, updates)
		require.Zero(t, requireFenceMiss(t, err, sendfence.ReasonStatusChanged).Applied)

		repository = newTestRepository(t, pool)
		request = assertPinnedReplay(t, repository, unit, ids, request, generation+1)
	}
}

func TestSendRequestMustBePinnedBeforeSending(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := newTestRepository(t, pool)
	ctx := t.Context()
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("pin-room", "pin")}})
	require.NoError(t, err)

	records, err := repository.ClaimDue(ctx, "owner", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 1)

	claims := recordClaims(records...)
	unit := records[0].SendUnitID
	require.Zero(t, requireFenceMiss(t, repository.BeginSendRequest(ctx, unit, claims, "owner", records[0].ClientRequestID, time.Minute), sendfence.ReasonRequestChanged).Applied)

	request, err := repository.PinSendRequest(ctx, unit, claims, "owner", records[0].ClientRequestID, SendRequest{Body: "new", Route: SendRouteText})
	require.NoError(t, err)
	require.NotNil(t, request)
}

func TestLegacySendRequestReplayDoesNotRepinRichText(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := newTestRepository(t, pool)
	ctx := t.Context()
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("legacy-room", "legacy")}})
	require.NoError(t, err)

	records, err := repository.ClaimDue(ctx, "owner", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 1)

	unit := records[0].SendUnitID
	claims := recordClaims(records...)
	requestID := records[0].ClientRequestID

	const body = "**원문** @사용자_이름"

	bodyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))

	// 구버전이 저장한 행을 복원한다. 새 pin은 markdown을 생성하지 않는다.
	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_send_units SET request_body=$2,request_route='markdown',
request_body_hash=$4,request_delivery_ids=$3,base_client_request_id=client_request_id WHERE id=$1`, unit, body, []int64{records[0].ID}, bodyHash)
	require.NoError(t, err)

	stored, err := repository.LoadSendRequest(ctx, unit, claims, "owner", requestID)
	require.NoError(t, err)

	replayed, err := newTestRepository(t, pool).PinSendRequest(ctx, unit, claims, "owner", requestID, SendRequest{Body: "새 본문", Route: SendRouteRichText})
	require.NoError(t, err)
	require.Equal(t, stored, replayed)
	require.Equal(t, SendRouteMarkdown, replayed.Route)
	require.Equal(t, body, replayed.Body)
	require.Equal(t, requestID, replayed.ClientRequestID)
}

func TestSendRequestCanPinAfterKnownUnsentFailure(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := newTestRepository(t, pool)
	ctx := t.Context()
	_, err := repository.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{receiptEnvelope("pre-render-room", "title")}})
	require.NoError(t, err)

	records, err := repository.ClaimDue(ctx, "owner", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 1)

	ids := []int64{records[0].ID}
	unit := records[0].SendUnitID
	requestID := records[0].ClientRequestID
	request, err := repository.LoadSendRequest(ctx, unit, recordClaims(records...), "owner", requestID)
	require.ErrorIs(t, err, ErrSendRequestUnpinned)
	require.Nil(t, request)
	require.NoError(t, repository.RouteFailures(ctx, []FailureUpdate{{ID: ids[0], AttemptCount: 1, TargetStatus: StatusRetry, Error: "template unavailable"}}, "owner"))

	records, err = repository.ClaimDue(ctx, "owner", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, records, 1)

	claims := recordClaims(records...)

	request, err = repository.PinSendRequest(ctx, unit, claims, "owner", requestID, SendRequest{Body: "recovered template", Route: SendRouteText})
	require.NoError(t, err)
	require.NotNil(t, request)
	require.NoError(t, repository.BeginSendRequest(ctx, unit, claims, "owner", request.ClientRequestID, time.Minute))
}

func pinConcurrentRequest(t *testing.T, repository *PgxRepository, unit int64, claims []DeliveryClaim, requestID string) *SendRequest {
	t.Helper()

	ctx := t.Context()
	// 동시 pin의 승자 하나가 이후 모든 request를 결정한다.
	results := make([]*SendRequest, 2)
	errs := make([]error, 2)

	var wg sync.WaitGroup

	for i := range 2 {
		wg.Go(func() {
			results[i], errs[i] = repository.PinSendRequest(ctx, unit, claims, "owner", requestID, SendRequest{Body: fmt.Sprintf("body-%d", i), Route: SendRouteRichText})
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

	claims := attemptClaims(generation, ids...)
	replay, err := repository.PinSendRequest(ctx, unit, claims, "owner", records[0].ClientRequestID, SendRequest{Body: "changed template", Route: SendRouteText})
	require.NoError(t, err)
	require.NotNil(t, replay)
	require.Equal(t, request.Body, replay.Body)
	require.Equal(t, request.Route, replay.Route)
	require.Equal(t, request.BodyHash, replay.BodyHash)
	require.Equal(t, request.BaseClientRequestID+fmt.Sprintf(":r%d", generation), replay.ClientRequestID)
	// 새 generation을 claim한 뒤 이전 요청 ID로는 발송 상태에 진입할 수 없다.
	require.Zero(t, requireFenceMiss(t, repository.BeginSendRequest(ctx, unit, claims, "owner", request.ClientRequestID, time.Minute), sendfence.ReasonRequestChanged).Applied)

	return replay
}

func TestReissueRequestRollsBackWholeUnitWhenMemberAttemptFenceFails(t *testing.T) {
	pool := dbtest.NewPool(t)
	repository := newTestRepository(t, pool)
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
	claims := recordClaims(records...)
	request, err := repository.PinSendRequest(ctx, unit, claims, "owner", records[0].ClientRequestID, SendRequest{Body: "body", Route: SendRouteText})
	require.NoError(t, err)
	require.NotNil(t, request)
	require.NoError(t, repository.BeginSendRequest(ctx, unit, claims, "owner", request.ClientRequestID, time.Minute))

	updates := []FailureUpdate{
		{ID: ids[0], AttemptCount: 1, TargetStatus: StatusRetry},
		{ID: ids[1], AttemptCount: 99, TargetStatus: StatusRetry},
	}
	miss := requireFenceMiss(t, repository.ReissueSendRequest(ctx, unit, "owner", request, updates), sendfence.ReasonVersionChanged)
	require.Zero(t, miss.Applied)
	require.Equal(t, []int64{ids[0], ids[1]}, miss.UnappliedDeliveryIDs())

	var count int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(id) FROM alarm_dispatch_deliveries WHERE send_unit_id=$1 AND status='sending' AND attempt_count=0 AND locked_by='owner'`, unit).Scan(&count))
	require.Equal(t, 2, count)

	var generation int

	require.NoError(t, pool.QueryRow(ctx, `SELECT request_generation FROM alarm_dispatch_send_units WHERE id=$1`, unit).Scan(&generation))
	require.Zero(t, generation)
}

// 같은 send unit의 재시도 전이는 한 문장 clock을 공유해 멤버 전원의 다음 시도 시각이 같다.
func TestFailureTransitionsShareOneClockAcrossUnitMembers(t *testing.T) {
	cases := map[string]func(t *testing.T, repository *PgxRepository, unit int64, records []*Record, request *SendRequest){
		"route-failures": func(t *testing.T, repository *PgxRepository, _ int64, records []*Record, _ *SendRequest) {
			t.Helper()

			require.NoError(t, repository.RouteFailures(t.Context(), unitRetryUpdates(records), "owner"))
		},
		"route-sending-failures": func(t *testing.T, repository *PgxRepository, unit int64, records []*Record, request *SendRequest) {
			t.Helper()

			require.NoError(t, repository.BeginSendRequest(t.Context(), unit, recordClaims(records...), "owner", request.ClientRequestID, time.Minute))
			require.NoError(t, repository.RouteSendingFailures(t.Context(), unitRetryUpdates(records), "owner"))
		},
		"reissue": func(t *testing.T, repository *PgxRepository, unit int64, records []*Record, request *SendRequest) {
			t.Helper()

			require.NoError(t, repository.BeginSendRequest(t.Context(), unit, recordClaims(records...), "owner", request.ClientRequestID, time.Minute))
			require.NoError(t, repository.ReissueSendRequest(t.Context(), unit, "owner", request, unitRetryUpdates(records)))
		},
	}

	for name, apply := range cases {
		t.Run(name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			repository := newTestRepository(t, pool)
			first := receiptEnvelope("clock-unit-room", "one")
			second := receiptEnvelope("clock-unit-room", "two")

			second.Notification.Stream.ID = "clock-unit-second"

			_, err := repository.InsertBatch(t.Context(), PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{first, second}})
			require.NoError(t, err)

			records, err := repository.ClaimDue(t.Context(), "owner", 10, time.Minute)
			require.NoError(t, err)
			require.Len(t, records, 2)

			unit := records[0].SendUnitID
			request, err := repository.PinSendRequest(t.Context(), unit, recordClaims(records...), "owner", records[0].ClientRequestID, SendRequest{Body: "body", Route: SendRouteText})
			require.NoError(t, err)

			apply(t, repository, unit, records, request)

			var distinct int

			var delay time.Duration

			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(DISTINCT next_attempt_at), max(next_attempt_at-updated_at)
				FROM alarm_dispatch_deliveries WHERE send_unit_id=$1 AND status='retry'`, unit).Scan(&distinct, &delay))
			require.Equal(t, 1, distinct, "unit members must share one next_attempt_at")
			require.Equal(t, 5*time.Second, delay)
		})
	}
}

func unitRetryUpdates(records []*Record) []FailureUpdate {
	updates := make([]FailureUpdate, 0, len(records))
	for _, record := range records {
		updates = append(updates, FailureUpdate{ID: record.ID, AttemptCount: record.AttemptCount + 1, RetryAfter: 5 * time.Second, TargetStatus: StatusRetry})
	}

	return updates
}
