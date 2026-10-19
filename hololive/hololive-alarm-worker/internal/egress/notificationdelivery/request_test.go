package notificationdelivery

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

const (
	testPreparedMarkdown = "markdown"
	testPreparedText     = "text"
)

type preparedSenderSpy struct {
	prepareErr          error
	body, route         string
	ids, bodies, routes []string
	err                 error
	onPrepare, onSend   func()
}

func (s *preparedSenderSpy) SendMessage(context.Context, string, string) error {
	return errors.New("unprepared send")
}

func (s *preparedSenderSpy) PrepareMessageRequest(context.Context, string, string) (string, string, error) {
	if s.onPrepare != nil {
		s.onPrepare()
	}

	return s.body, s.route, s.prepareErr
}

func (s *preparedSenderSpy) SendPreparedMessage(_ context.Context, _, body, route, id string) error {
	s.ids = append(s.ids, id)
	s.bodies = append(s.bodies, body)
	s.routes = append(s.routes, route)

	if s.onSend != nil {
		s.onSend()
	}

	return s.err
}

func TestGenericRequestSnapshotAndBoundedReissueSurviveRestart(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "snapshot", "room", "source"))

	conflict := testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`)
	sender := &preparedSenderSpy{body: "original final body", route: testPreparedMarkdown, err: conflict}
	cfg := testDispatcherConfig()

	cfg.RetryBackoff = time.Nanosecond

	for generation := range 3 {
		d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
		d.processReady(ctx)
		require.Len(t, sender.ids, generation+1)

		if generation == 0 {
			sender.body = "template changed"
			sender.route = testPreparedText
		}
	}

	require.Equal(t, []string{sender.ids[0], sender.ids[0] + ":r1", sender.ids[0] + ":r2"}, sender.ids)
	require.Equal(t, []string{"original final body", "original final body", "original final body"}, sender.bodies)
	require.Equal(t, []string{testPreparedMarkdown, testPreparedMarkdown, testPreparedMarkdown}, sender.routes)
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, domain.DeliveryStatusFailed))
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "snapshot", "room", "new source"))

	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processReady(ctx)
	require.Len(t, sender.ids, 3, "generation exhaustion must survive enqueue revive")
}

func TestGenericRequestOrdinaryFailedRearmPreservesSnapshot(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "rearm", "room", "source"))

	sender := &preparedSenderSpy{body: "pinned body", route: testPreparedText, err: errors.New("known failure")}
	cfg := testDispatcherConfig()

	cfg.MaxRetries = 1

	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processReady(ctx)
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, domain.DeliveryStatusFailed))
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "rearm", "room", "different source"))

	sender.body = "different rendered body"
	sender.route = testPreparedMarkdown
	sender.err = nil
	d = mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processReady(ctx)
	require.Len(t, sender.ids, 2)
	require.Equal(t, sender.ids[0], sender.ids[1])
	require.Equal(t, []string{"pinned body", "pinned body"}, sender.bodies)
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, domain.DeliveryStatusSent))
}

func requireRequestFenceMiss(t *testing.T, err error, cause requestFenceCause) {
	t.Helper()

	miss, ok := errors.AsType[*requestFenceMissError](err)
	require.Truef(t, ok, "want request fence miss, got %v", err)
	require.Equal(t, cause, miss.Cause)
}

func TestGenericRequestSnapshotCASAndQuarantineFence(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "cas", "room", "source"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	id := items[0].ID
	request := &deliverypayload.Request{Body: "pinned", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	requireRequestFenceMiss(t, repo.saveRequest(ctx, id, testWorkerB, nil, request), requestFenceOwnerChanged)
	require.NoError(t, repo.saveRequest(ctx, id, testWorkerA, nil, request))

	changed := *request

	changed.Body = "racing body"
	requireRequestFenceMiss(t, repo.saveRequest(ctx, id, testWorkerA, nil, &changed), requestFenceRequestChanged)
	markOutboxSending(ctx, t, repo, &items[0])

	saved, err := repo.MarkQuarantined(ctx, id, testWorkerB, "foreign")
	require.NoError(t, err)
	require.False(t, saved)

	saved, err = repo.MarkQuarantined(ctx, id, testWorkerA, "unknown")
	require.NoError(t, err)
	require.True(t, saved)

	saved, err = repo.MarkFailed(ctx, id, testWorkerA, 0, 3, time.Minute, "late failure")
	require.NoError(t, err)
	require.False(t, saved)
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "cas", "room", "changed"))
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, deliveryStatusQuarantined))
}

func TestGenericGenerationSavedBeforeRetryCanBeReplayed(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "replay", "room", "source"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	previous := &deliverypayload.Request{Body: "pinned", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	require.NoError(t, repo.saveRequest(ctx, items[0].ID, testWorkerA, nil, previous))
	markOutboxSending(ctx, t, repo, &items[0])

	next := *previous

	next.Generation = 1
	require.NoError(t, repo.reissueFailedRequest(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, previous, &next, 3, 0, "pre-handoff failure"))

	// 별도 MarkFailed 호출 없이 새 dispatcher가 commit된 세대와 retry 상태를 복구합니다.

	sender := &preparedSenderSpy{body: "new config", route: testPreparedMarkdown}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processReady(ctx)
	require.Equal(t, []string{previous.BaseID + ":r1"}, sender.ids)
	require.Equal(t, []string{"pinned"}, sender.bodies)
}

func TestGenericPayloadMismatchDoesNotReissue(t *testing.T) {
	var snapshots []*deliverypayload.Request

	repo := &mockDeliveryRepository{saveRequestFn: func(_ context.Context, _ int64, _ string, _, next *deliverypayload.Request) error {
		snapshots = append(snapshots, next)
		return nil
	}}
	sender := &preparedSenderSpy{body: "body", route: testPreparedText, err: testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_PAYLOAD_MISMATCH"}`)}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processItem(t.Context(), &domain.NotificationDeliveryOutbox{ID: 1, Payload: makePayload(t, "source")})
	require.Len(t, snapshots, 1)
	require.Equal(t, 0, snapshots[0].Generation)
}

func TestGenericRequestGenerationCASRejectsStaleWriter(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "generation-cas", "room", "source"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	id := items[0].ID
	previous := &deliverypayload.Request{Body: "body", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	require.NoError(t, repo.saveRequest(ctx, id, testWorkerA, nil, previous))
	markOutboxSending(ctx, t, repo, &items[0])

	next := *previous

	next.Generation = 1

	results := make(chan error, 2)

	for range 2 {
		go func() {
			results <- repo.saveRequest(ctx, id, testWorkerA, previous, &next)
		}()
	}

	var (
		applied int
		missed  error
	)

	for range 2 {
		if err := <-results; err != nil {
			missed = err
		} else {
			applied++
		}
	}

	require.Equal(t, 1, applied)
	requireRequestFenceMiss(t, missed, requestFenceRequestChanged)

	var raw []byte

	require.NoError(t, repo.pool.QueryRow(ctx, "SELECT payload FROM notification_delivery_outbox WHERE id=$1", id).Scan(&raw))

	var p deliverypayload.Payload

	require.NoError(t, jsonv2.Unmarshal(raw, &p))
	require.Equal(t, 1, p.Request.Generation)
}

func TestGenericPinnedFailedRearmPreservesOriginalRoom(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	// content_id는 period_key와 room_id의 연결이므로 두 입력이 같은 conflict key를 만들 수 있습니다.
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "period:part", "room", "source"))

	sender := &preparedSenderSpy{body: "pinned body", route: testPreparedText, err: errors.New("known failure")}
	cfg := testDispatcherConfig()

	cfg.MaxRetries = 1

	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processReady(ctx)
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "period", "part:room", "changed source"))

	var room string

	require.NoError(t, repo.pool.QueryRow(ctx, "SELECT room_id FROM notification_delivery_outbox").Scan(&room))
	require.Equal(t, "room", room)
}

func TestGenericPreparationFailureRetriesWithoutSending(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "prepare-retry", "room", "source"))

	sender := &preparedSenderSpy{body: "body", route: testPreparedText, prepareErr: errors.New("prepare unavailable")}
	cfg := testDispatcherConfig()

	cfg.RetryBackoff = time.Nanosecond

	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processReady(ctx)
	require.Empty(t, sender.ids)
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, domain.DeliveryStatusPending))

	sender.prepareErr = nil
	d = mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processReady(ctx)
	require.Len(t, sender.ids, 1)
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, domain.DeliveryStatusSent))
}

func TestGenericUnknownEvidencePreventsJoinedConflictReissue(t *testing.T) {
	for _, unknown := range []error{
		sendoutcome.ErrHandoffOutcomeUnknown, context.DeadlineExceeded, context.Canceled,
		testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_OUTCOME_UNKNOWN"}`),
		errors.Join(sendoutcome.ErrHandoffFailed, &iris.TransportError{Op: "post", Err: io.ErrUnexpectedEOF}),
	} {
		t.Run(unknown.Error(), func(t *testing.T) {
			repo := testRepository(t)
			ctx := t.Context()
			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "joined-conflict", "room", "source"))

			sender := &preparedSenderSpy{body: "pinned", route: testPreparedText, err: errors.Join(testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`), unknown)}
			d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
			d.processReady(ctx)
			d.processReady(ctx)
			require.Len(t, sender.ids, 1)
			require.EqualValues(t, 1, countByStatus(ctx, t, repo, deliveryStatusQuarantined))

			var generation int

			require.NoError(t, repo.pool.QueryRow(ctx, `SELECT (payload->'request'->>'generation')::int FROM notification_delivery_outbox WHERE period_key='joined-conflict'`).Scan(&generation))
			require.Zero(t, generation)
		})
	}
}

// 저장된 요청을 Go가 다시 인코딩하면 다른 jsonb가 되는 표현으로 고정해, 디코딩한 요청은 같지만 재발급 CAS의 요청 비교만
// 어긋나게 한다. 소유자·상태·시도 횟수는 그대로라 dispatcher가 아직 행을 소유한다.
func pinDriftedRequest(ctx context.Context, t *testing.T, repo *outboxFixture, periodKey string) string {
	t.Helper()

	baseID := notificationDeliveryClientRequestID(&domain.NotificationDeliveryOutbox{ID: 1})
	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox
		SET payload = jsonb_set(payload, '{request}', jsonb_build_object('body', 'pinned', 'route', $2::text, 'base_id', $3::text, 'generation', 0, 'exhausted', false))
		WHERE period_key = $1`, periodKey, testPreparedText, baseID)
	require.NoError(t, err)

	return baseID
}

func requestFenceMissCount(operation requestFenceOperation, cause requestFenceCause, result string) float64 {
	return promtestutil.ToFloat64(notificationDeliveryRequestFenceMissTotal.WithLabelValues(string(operation), string(cause), result))
}

func TestGenericReissueFenceMissWithHeldOwnershipFailsWithoutSendingResidue(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "reissue-fence-held", "room", "source"))

	baseID := pinDriftedRequest(ctx, t, repo, "reissue-fence-held")
	sender := &preparedSenderSpy{err: testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`)}
	before := requestFenceMissCount(requestFenceReissue, requestFenceRequestChanged, requestFenceResultFailed)
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processReady(ctx)
	require.Equal(t, []string{baseID}, sender.ids)
	require.InDelta(t, before+1, requestFenceMissCount(requestFenceReissue, requestFenceRequestChanged, requestFenceResultFailed), 0)

	var (
		status     string
		attempts   int
		lockedBy   *string
		generation int
		exhausted  bool
	)

	readRow := func() {
		t.Helper()

		require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status, attempt_count, locked_by, (payload->'request'->>'generation')::int,
			COALESCE((payload->'request'->>'exhausted')::boolean, false)
			FROM notification_delivery_outbox WHERE period_key = 'reissue-fence-held'`).Scan(&status, &attempts, &lockedBy, &generation, &exhausted))
	}

	readRow()
	require.Equal(t, string(domain.DeliveryStatusFailed), status)
	require.Equal(t, 1, attempts)
	require.Nil(t, lockedBy)
	require.Zero(t, generation, "a missed reissue must not persist the next generation")
	require.True(t, exhausted)
	require.Zero(t, countByStatus(ctx, t, repo, deliveryStatusSending))

	// 같은 키를 다시 적재해도 producer rearm이 소진된 요청을 PENDING으로 되살리지 않는다.
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "reissue-fence-held", "room", "new source"))
	d.processReady(ctx)
	readRow()
	require.Equal(t, string(domain.DeliveryStatusFailed), status)
	require.Equal(t, 1, attempts)
	require.Len(t, sender.ids, 1)
}

func TestGenericReissueFenceMissLeavesForeignTakeoverUntouched(t *testing.T) {
	for _, tc := range []struct {
		name     string
		takeover func(context.Context, *testing.T, *outboxFixture, string)
		cause    requestFenceCause
		status   domain.DeliveryOutboxStatus
		lockedBy *string
		reason   *string
	}{
		{
			name: "stale_sweep",
			takeover: func(ctx context.Context, t *testing.T, repo *outboxFixture, periodKey string) {
				t.Helper()

				_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET sending_started_at = sending_started_at - INTERVAL '1 hour' WHERE period_key = $1`, periodKey)
				require.NoError(t, err)

				swept, err := repo.QuarantineStaleSending(ctx, time.Minute, 10)
				require.NoError(t, err)
				require.EqualValues(t, 1, swept)
			},
			cause:  requestFenceStatusChanged,
			status: deliveryStatusQuarantined,
			reason: new(staleSendingFailureReason),
		},
		{
			name: "foreign_owner",
			takeover: func(ctx context.Context, t *testing.T, repo *outboxFixture, periodKey string) {
				t.Helper()

				_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET locked_by = $2 WHERE period_key = $1`, periodKey, testWorkerB)
				require.NoError(t, err)
			},
			cause:    requestFenceOwnerChanged,
			status:   deliveryStatusSending,
			lockedBy: new(testWorkerB),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testRepository(t)
			ctx := t.Context()
			periodKey := "fence-" + tc.name
			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, periodKey, "room", "source"))

			sender := &preparedSenderSpy{
				body: "conflicted", route: testPreparedText,
				err:    testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`),
				onSend: func() { tc.takeover(ctx, t, repo, periodKey) },
			}

			before := requestFenceMissCount(requestFenceReissue, tc.cause, requestFenceResultFenceLost)

			d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
			d.processReady(ctx)
			require.Len(t, sender.ids, 1)
			require.InDelta(t, before+1, requestFenceMissCount(requestFenceReissue, tc.cause, requestFenceResultFenceLost), 0)

			var (
				status     string
				attempts   int
				lockedBy   *string
				reason     *string
				generation int
			)

			require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status, attempt_count, locked_by, error, (payload->'request'->>'generation')::int
				FROM notification_delivery_outbox WHERE period_key = $1`, periodKey).Scan(&status, &attempts, &lockedBy, &reason, &generation))
			require.Equal(t, string(tc.status), status)
			require.Zero(t, attempts)
			require.Equal(t, tc.lockedBy, lockedBy)
			require.Equal(t, tc.reason, reason)
			require.Zero(t, generation)
		})
	}
}

// 요청을 고정하기 전 fence 미스는 발송하지 않는다. 소유권이 남았으면 일반 실패로 claim을 반납하고, 다른 worker가
// 재claim한 행은 그대로 둔다.
func TestGenericSaveRequestFenceMissNeverSendsAndRespectsForeignClaim(t *testing.T) {
	for _, tc := range []struct {
		name      string
		interfere func(context.Context, *testing.T, *outboxFixture, string)
		cause     requestFenceCause
		result    string
		lockedBy  *string
		attempts  int
	}{
		{
			name: "reclaimed",
			interfere: func(ctx context.Context, t *testing.T, repo *outboxFixture, periodKey string) {
				t.Helper()

				_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET lock_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE period_key = $1`, periodKey)
				require.NoError(t, err)
				require.Len(t, claimItems(ctx, t, repo, testWorkerB, 1), 1)
			},
			cause:    requestFenceOwnerChanged,
			result:   requestFenceResultFenceLost,
			lockedBy: new(testWorkerB),
		},
		{
			name: "pinned_other",
			interfere: func(ctx context.Context, t *testing.T, repo *outboxFixture, periodKey string) {
				t.Helper()

				pinDriftedRequest(ctx, t, repo, periodKey)
			},
			cause:    requestFenceRequestChanged,
			result:   requestFenceResultFailed,
			attempts: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testRepository(t)
			ctx := t.Context()
			periodKey := "save-" + tc.name
			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, periodKey, "room", "source"))

			sender := &preparedSenderSpy{body: "rendered", route: testPreparedText, onPrepare: func() { tc.interfere(ctx, t, repo, periodKey) }}

			before := requestFenceMissCount(requestFenceSave, tc.cause, tc.result)

			d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
			d.processReady(ctx)
			require.Empty(t, sender.ids)
			require.InDelta(t, before+1, requestFenceMissCount(requestFenceSave, tc.cause, tc.result), 0)

			var (
				status   string
				attempts int
				lockedBy *string
				body     *string
			)

			require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status, attempt_count, locked_by, payload->'request'->>'body'
				FROM notification_delivery_outbox WHERE period_key = $1`, periodKey).Scan(&status, &attempts, &lockedBy, &body))
			require.Equal(t, string(domain.DeliveryStatusPending), status)
			require.Equal(t, tc.attempts, attempts)
			require.Equal(t, tc.lockedBy, lockedBy)
			require.NotEqual(t, new("rendered"), body, "a missed save must not pin the worker's request")
		})
	}
}

func TestRequestFenceMissReportsObservedCause(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "fence-cause", "room", "source"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	id := items[0].ID
	request := &deliverypayload.Request{Body: "fenced", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET lock_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1`, id)
	require.NoError(t, err)
	requireRequestFenceMiss(t, repo.saveRequest(ctx, id, testWorkerA, nil, request), requestFenceLeaseExpired)

	reclaimed := claimItems(ctx, t, repo, testWorkerB, 1)
	require.Len(t, reclaimed, 1)
	requireRequestFenceMiss(t, repo.saveRequest(ctx, id, testWorkerA, nil, request), requestFenceOwnerChanged)

	require.NoError(t, repo.saveRequest(ctx, id, testWorkerB, nil, request))

	ok, err := repo.MarkSending(ctx, id, testWorkerB, testLease)
	require.NoError(t, err)
	require.True(t, ok)

	next := *request

	next.Generation = 1
	_, err = repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET sending_started_at = sending_started_at - INTERVAL '1 hour' WHERE id = $1`, id)
	require.NoError(t, err)

	_, err = repo.QuarantineStaleSending(ctx, time.Minute, 10)
	require.NoError(t, err)
	requireRequestFenceMiss(t, repo.reissueFailedRequest(ctx, id, testWorkerB, reclaimed[0].AttemptCount, request, &next, 1, time.Minute, "rejected"), requestFenceStatusChanged)

	_, err = repo.pool.Exec(ctx, `DELETE FROM notification_delivery_outbox WHERE id = $1`, id)
	require.NoError(t, err)
	requireRequestFenceMiss(t, repo.reissueFailedRequest(ctx, id, testWorkerB, reclaimed[0].AttemptCount, request, &next, 1, time.Minute, "rejected"), requestFenceRowMissing)
}
