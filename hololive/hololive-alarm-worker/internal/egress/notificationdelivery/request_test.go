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

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
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

func TestGenericRequestSnapshotCASAndQuarantineFence(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "cas", "room", "source"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	id, attempt := items[0].ID, items[0].AttemptCount
	request := &deliverypayload.Request{Body: "pinned", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	requireFenceMiss(t, repo.saveRequest(ctx, id, testWorkerB, attempt, request), actionSaveRequest, sendfence.ReasonOwnerChanged)
	require.NoError(t, repo.saveRequest(ctx, id, testWorkerA, attempt, request))

	changed := *request

	changed.Body = "racing body"
	requireFenceMiss(t, repo.saveRequest(ctx, id, testWorkerA, attempt, &changed), actionSaveRequest, sendfence.ReasonRequestChanged)
	markOutboxSending(ctx, t, repo, &items[0])

	requireFenceMiss(t, repo.MarkQuarantined(ctx, id, testWorkerB, attempt, "foreign"), actionMarkQuarantined, sendfence.ReasonOwnerChanged)
	require.NoError(t, repo.MarkQuarantined(ctx, id, testWorkerA, attempt, "unknown"))
	requireFenceMiss(t, repo.MarkFailed(ctx, id, testWorkerA, attempt, 3, time.Minute, "late failure"), actionMarkFailed, sendfence.ReasonStatusChanged)
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
	require.NoError(t, repo.saveRequest(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, previous))
	markOutboxSending(ctx, t, repo, &items[0])
	require.NoError(t, repo.reissueFailedRequest(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, previous.Generation, false, 3, 0, "pre-handoff failure"))

	// 별도 MarkFailed 호출 없이 새 dispatcher가 commit된 세대와 retry 상태를 복구합니다.

	sender := &preparedSenderSpy{body: "new config", route: testPreparedMarkdown}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processReady(ctx)
	require.Equal(t, []string{previous.BaseID + ":r1"}, sender.ids)
	require.Equal(t, []string{"pinned"}, sender.bodies)
}

func TestGenericPayloadMismatchDoesNotReissue(t *testing.T) {
	var (
		pinned   []*deliverypayload.Request
		reissued int
	)

	repo := &mockDeliveryRepository{
		saveRequestFn: func(_ context.Context, _ int64, _ string, _ int, request *deliverypayload.Request) error {
			pinned = append(pinned, request)
			return nil
		},
		reissueFn: func(context.Context, int64, string, int, int, bool) error {
			reissued++
			return nil
		},
	}
	sender := &preparedSenderSpy{body: "body", route: testPreparedText, err: testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_PAYLOAD_MISMATCH"}`)}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processItem(t.Context(), &domain.NotificationDeliveryOutbox{ID: 1, Payload: makePayload(t, "source")})
	require.Len(t, pinned, 1)
	require.Zero(t, pinned[0].Generation)
	require.Zero(t, reissued, "payload mismatch must not advance the request generation")
	require.Len(t, sender.ids, 1)
}

// 다른 writer가 세대를 먼저 기록했으면 이전 세대를 기대한 재발급은 행을 바꾸지 않고 request_changed가 된다.
func TestGenericRequestGenerationFenceRejectsStaleGeneration(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "generation-fence", "room", "source"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	id, attempt := items[0].ID, items[0].AttemptCount
	request := &deliverypayload.Request{Body: "body", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	require.NoError(t, repo.saveRequest(ctx, id, testWorkerA, attempt, request))
	markOutboxSending(ctx, t, repo, &items[0])

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET payload = jsonb_set(payload, '{request,generation}', '1'::jsonb) WHERE id = $1`, id)
	require.NoError(t, err)

	before := readOwnerRow(ctx, t, repo, id)
	requireFenceMiss(t, repo.reissueFailedRequest(ctx, id, testWorkerA, attempt, 0, false, 3, time.Minute, "stale generation"), actionReissueRequest, sendfence.ReasonRequestChanged)
	require.Equal(t, before, readOwnerRow(ctx, t, repo, id))
	require.Equal(t, 1, storedRequest(ctx, t, repo, id).Generation)

	// 같은 세대를 기대한 두 재발급이 겹쳐도 세대는 한 번만 오른다. 늦은 쪽은 앞선 전이가 SENDING을 끝낸 행을 본다.
	require.NoError(t, repo.reissueFailedRequest(ctx, id, testWorkerA, attempt, 1, false, 3, time.Minute, "first"))
	requireFenceMiss(t, repo.reissueFailedRequest(ctx, id, testWorkerA, attempt, 1, false, 3, time.Minute, "second"), actionReissueRequest, sendfence.ReasonStatusChanged)
	require.Equal(t, 2, storedRequest(ctx, t, repo, id).Generation)
}

func TestGenericConcurrentReissueAdvancesGenerationOnce(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "generation-cas", "room", "source"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	id, attempt := items[0].ID, items[0].AttemptCount
	request := &deliverypayload.Request{Body: "body", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	require.NoError(t, repo.saveRequest(ctx, id, testWorkerA, attempt, request))
	markOutboxSending(ctx, t, repo, &items[0])

	results := make(chan error, 2)

	for range 2 {
		go func() {
			results <- repo.reissueFailedRequest(ctx, id, testWorkerA, attempt, 0, false, 3, time.Minute, "conflict")
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
	requireFenceMiss(t, missed, actionReissueRequest, sendfence.ReasonStatusChanged)
	require.Equal(t, 1, storedRequest(ctx, t, repo, id).Generation)
}

func storedRequest(ctx context.Context, t *testing.T, repo *outboxFixture, id int64) deliverypayload.Request {
	t.Helper()

	var raw []byte

	require.NoError(t, repo.pool.QueryRow(ctx, "SELECT payload FROM notification_delivery_outbox WHERE id=$1", id).Scan(&raw))

	var p deliverypayload.Payload

	require.NoError(t, jsonv2.Unmarshal(raw, &p))
	require.NotNil(t, p.Request)

	return *p.Request
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

// pinForeignRequest는 다른 writer가 이 worker보다 먼저 요청을 고정한 상태를 만든다. 소유자·상태·시도 횟수는 그대로라
// dispatcher가 아직 행을 소유한다.
func pinForeignRequest(ctx context.Context, t *testing.T, repo *outboxFixture, periodKey string) {
	t.Helper()

	baseID := notificationDeliveryClientRequestID(&domain.NotificationDeliveryOutbox{ID: 1})
	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox
		SET payload = jsonb_set(payload, '{request}', jsonb_build_object('body', 'pinned', 'route', $2::text, 'base_id', $3::text, 'generation', 0))
		WHERE period_key = $1`, periodKey, testPreparedText, baseID)
	require.NoError(t, err)
}

func fenceMissCount(action string, reason sendfence.Reason, result string) float64 {
	return promtestutil.ToFloat64(notificationDeliveryFenceMissTotal.WithLabelValues(action, string(reason), result))
}

// 발송 중 다른 writer가 세대를 앞서 기록했으면 409 재발급은 새 세대를 쓰지 못한다. 소유권은 남았으므로 같은 ID 반복
// 충돌을 피하려고 저장된 요청을 소진으로 표시한 종료 FAILED로 끝내며 SENDING을 남기지 않는다.
func TestGenericReissueFenceMissWithHeldOwnershipFailsWithoutSendingResidue(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "reissue-fence-held", "room", "source"))

	sender := &preparedSenderSpy{
		body: "held body", route: testPreparedText,
		err: testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`),
		onSend: func() {
			_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET payload = jsonb_set(payload, '{request,generation}', '2'::jsonb)
				WHERE period_key = 'reissue-fence-held'`)
			require.NoError(t, err)
		},
	}
	before := fenceMissCount(actionReissueRequest, sendfence.ReasonRequestChanged, fenceMissResultFailed)
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processReady(ctx)
	require.Len(t, sender.ids, 1)
	require.InDelta(t, before+1, fenceMissCount(actionReissueRequest, sendfence.ReasonRequestChanged, fenceMissResultFailed), 0)

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
	require.Equal(t, 2, generation, "a missed reissue must not persist its own next generation")
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
		reason   sendfence.Reason
		status   domain.DeliveryOutboxStatus
		lockedBy *string
		errText  *string
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
			reason:  sendfence.ReasonStatusChanged,
			status:  deliveryStatusQuarantined,
			errText: new(staleSendingFailureReason),
		},
		{
			name: "foreign_owner",
			takeover: func(ctx context.Context, t *testing.T, repo *outboxFixture, periodKey string) {
				t.Helper()

				_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET locked_by = $2 WHERE period_key = $1`, periodKey, testWorkerB)
				require.NoError(t, err)
			},
			reason:   sendfence.ReasonOwnerChanged,
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

			before := fenceMissCount(actionReissueRequest, tc.reason, fenceMissResultFenceLost)

			d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
			d.processReady(ctx)
			require.Len(t, sender.ids, 1)
			require.InDelta(t, before+1, fenceMissCount(actionReissueRequest, tc.reason, fenceMissResultFenceLost), 0)

			var (
				status     string
				attempts   int
				lockedBy   *string
				errText    *string
				generation int
			)

			require.NoError(t, repo.pool.QueryRow(ctx, `SELECT status, attempt_count, locked_by, error, (payload->'request'->>'generation')::int
				FROM notification_delivery_outbox WHERE period_key = $1`, periodKey).Scan(&status, &attempts, &lockedBy, &errText, &generation))
			require.Equal(t, string(tc.status), status)
			require.Zero(t, attempts)
			require.Equal(t, tc.lockedBy, lockedBy)
			require.Equal(t, tc.errText, errText)
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
		reason    sendfence.Reason
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
			reason:   sendfence.ReasonOwnerChanged,
			result:   fenceMissResultFenceLost,
			lockedBy: new(testWorkerB),
		},
		{
			name: "pinned_other",
			interfere: func(ctx context.Context, t *testing.T, repo *outboxFixture, periodKey string) {
				t.Helper()

				pinForeignRequest(ctx, t, repo, periodKey)
			},
			reason:   sendfence.ReasonRequestChanged,
			result:   fenceMissResultFailed,
			attempts: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testRepository(t)
			ctx := t.Context()
			periodKey := "save-" + tc.name
			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, periodKey, "room", "source"))

			sender := &preparedSenderSpy{body: "rendered", route: testPreparedText, onPrepare: func() { tc.interfere(ctx, t, repo, periodKey) }}

			before := fenceMissCount(actionSaveRequest, tc.reason, tc.result)

			d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
			d.processReady(ctx)
			require.Empty(t, sender.ids)
			require.InDelta(t, before+1, fenceMissCount(actionSaveRequest, tc.reason, tc.result), 0)

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

func TestRequestFenceMissReportsObservedReason(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "fence-cause", "room", "source"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	id := items[0].ID
	request := &deliverypayload.Request{Body: "fenced", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}

	expireLease(ctx, t, repo, id)
	requireFenceMiss(t, repo.saveRequest(ctx, id, testWorkerA, items[0].AttemptCount, request), actionSaveRequest, sendfence.ReasonLeaseExpired)

	reclaimed := claimItems(ctx, t, repo, testWorkerB, 1)
	require.Len(t, reclaimed, 1)
	requireFenceMiss(t, repo.saveRequest(ctx, id, testWorkerA, items[0].AttemptCount, request), actionSaveRequest, sendfence.ReasonOwnerChanged)

	attempt := reclaimed[0].AttemptCount
	require.NoError(t, repo.saveRequest(ctx, id, testWorkerB, attempt, request))
	require.NoError(t, repo.MarkSending(ctx, id, testWorkerB, attempt, testLease))

	_, err := repo.pool.Exec(ctx, `UPDATE notification_delivery_outbox SET sending_started_at = sending_started_at - INTERVAL '1 hour' WHERE id = $1`, id)
	require.NoError(t, err)

	_, err = repo.QuarantineStaleSending(ctx, time.Minute, 10)
	require.NoError(t, err)
	requireFenceMiss(t, repo.reissueFailedRequest(ctx, id, testWorkerB, attempt, 0, false, 1, time.Minute, "rejected"), actionReissueRequest, sendfence.ReasonStatusChanged)

	_, err = repo.pool.Exec(ctx, `DELETE FROM notification_delivery_outbox WHERE id = $1`, id)
	require.NoError(t, err)
	requireFenceMiss(t, repo.reissueFailedRequest(ctx, id, testWorkerB, attempt, 0, false, 1, time.Minute, "rejected"), actionReissueRequest, sendfence.ReasonMissing)
}
