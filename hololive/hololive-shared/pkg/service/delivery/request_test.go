package delivery

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
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
}

func (s *preparedSenderSpy) SendMessage(context.Context, string, string) error {
	return errors.New("unprepared send")
}

func (s *preparedSenderSpy) PrepareMessageRequest(context.Context, string, string) (string, string, error) {
	return s.body, s.route, s.prepareErr
}

func (s *preparedSenderSpy) SendPreparedMessage(_ context.Context, _, body, route, id string) error {
	s.ids = append(s.ids, id)
	s.bodies = append(s.bodies, body)
	s.routes = append(s.routes, route)

	return s.err
}

func TestGenericRequestSnapshotAndBoundedReissueSurviveRestart(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "snapshot", "room", "source"))

	conflict := &iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`}
	sender := &preparedSenderSpy{body: "original final body", route: testPreparedMarkdown, err: conflict}
	cfg := testDispatcherConfig()

	cfg.RetryBackoff = time.Nanosecond

	for generation := range 3 {
		d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
		d.processOnce(ctx)
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
	d.processOnce(ctx)
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
	d.processOnce(ctx)
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, domain.DeliveryStatusFailed))
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "rearm", "room", "different source"))

	sender.body = "different rendered body"
	sender.route = testPreparedMarkdown
	sender.err = nil
	d = mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processOnce(ctx)
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

	id := items[0].ID
	request := &preparedMessage{Body: "pinned", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	saved, err := repo.saveRequest(ctx, id, testWorkerB, nil, request)
	require.NoError(t, err)
	require.False(t, saved)

	saved, err = repo.saveRequest(ctx, id, testWorkerA, nil, request)
	require.NoError(t, err)
	require.True(t, saved)

	changed := *request

	changed.Body = "racing body"
	saved, err = repo.saveRequest(ctx, id, testWorkerA, nil, &changed)
	require.NoError(t, err)
	require.False(t, saved)
	markOutboxSending(ctx, t, repo, &items[0])

	saved, err = repo.MarkQuarantined(ctx, id, testWorkerB, "foreign")
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

func TestGenericLegacyAttemptDoesNotInventPriorRequest(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "legacy", "room", "source"))

	_, err := repo.pool.Exec(ctx, "UPDATE notification_delivery_outbox SET attempt_count=1")
	require.NoError(t, err)

	sender := &preparedSenderSpy{body: "current template", route: testPreparedText}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processOnce(ctx)
	require.Empty(t, sender.ids)
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, deliveryStatusQuarantined))
}

func TestGenericGenerationSavedBeforeRetryCanBeReplayed(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "replay", "room", "source"))

	items := fetchAndLockItems(ctx, t, repo)
	require.Len(t, items, 1)

	previous := &preparedMessage{Body: "pinned", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	saved, err := repo.saveRequest(ctx, items[0].ID, testWorkerA, nil, previous)
	require.NoError(t, err)
	require.True(t, saved)
	markOutboxSending(ctx, t, repo, &items[0])

	next := *previous

	next.Generation = 1
	saved, err = repo.reissueFailedRequest(ctx, items[0].ID, testWorkerA, items[0].AttemptCount, previous, &next, 3, 0, "pre-handoff failure")
	require.NoError(t, err)
	require.True(t, saved)

	// 별도 MarkFailed 호출 없이 새 dispatcher가 commit된 세대와 retry 상태를 복구합니다.

	sender := &preparedSenderSpy{body: "new config", route: testPreparedMarkdown}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processOnce(ctx)
	require.Equal(t, []string{previous.BaseID + ":r1"}, sender.ids)
	require.Equal(t, []string{"pinned"}, sender.bodies)
}

func TestGenericPayloadMismatchDoesNotReissue(t *testing.T) {
	var snapshots []*preparedMessage

	repo := &mockDeliveryRepository{saveRequestFn: func(_ context.Context, _ int64, _ string, _, next *preparedMessage) (bool, error) {
		snapshots = append(snapshots, next)
		return true, nil
	}}
	sender := &preparedSenderSpy{body: "body", route: testPreparedText, err: &iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_PAYLOAD_MISMATCH"}`}}
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
	previous := &preparedMessage{Body: "body", Route: testPreparedText, BaseID: notificationDeliveryClientRequestID(&items[0])}
	saved, err := repo.saveRequest(ctx, id, testWorkerA, nil, previous)
	require.NoError(t, err)
	require.True(t, saved)
	markOutboxSending(ctx, t, repo, &items[0])

	next := *previous

	next.Generation = 1

	results := make(chan bool, 2)

	for range 2 {
		go func() {
			saved, err := repo.saveRequest(ctx, id, testWorkerA, previous, &next)
			if err != nil {
				t.Errorf("save request: %v", err)
			}

			results <- saved
		}()
	}

	got := []bool{<-results, <-results}
	require.Len(t, slices.DeleteFunc(got, func(v bool) bool { return !v }), 1)

	var raw []byte

	require.NoError(t, repo.pool.QueryRow(ctx, "SELECT payload FROM notification_delivery_outbox WHERE id=$1", id).Scan(&raw))

	var p outboxPayload

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
	d.processOnce(ctx)
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "period", "part:room", "changed source"))

	var room string

	require.NoError(t, repo.pool.QueryRow(ctx, "SELECT room_id FROM notification_delivery_outbox").Scan(&room))
	require.Equal(t, "room", room)
}

func TestGenericLegacyFailedRearmCannotDiscardPriorAttemptEvidence(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "legacy-failed", "room", "old source"))

	_, err := repo.pool.Exec(ctx, "UPDATE notification_delivery_outbox SET status='FAILED', attempt_count=3")
	require.NoError(t, err)
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "legacy-failed", "room", "new source"))

	var (
		status   string
		attempts int
	)

	require.NoError(t, repo.pool.QueryRow(ctx, "SELECT status, attempt_count FROM notification_delivery_outbox").Scan(&status, &attempts))
	require.Equal(t, "FAILED", status)
	require.Equal(t, 3, attempts)

	sender := &preparedSenderSpy{body: "new config", route: testPreparedText}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.processOnce(ctx)
	require.Empty(t, sender.ids)
}

func TestGenericPreparationFailureKeepsUnsentEvidenceForRetry(t *testing.T) {
	repo := testRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "prepare-retry", "room", "source"))

	sender := &preparedSenderSpy{body: "body", route: testPreparedText, prepareErr: errors.New("prepare unavailable")}
	cfg := testDispatcherConfig()

	cfg.RetryBackoff = time.Nanosecond

	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processOnce(ctx)
	require.Empty(t, sender.ids)
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, domain.DeliveryStatusPending))

	sender.prepareErr = nil
	d = mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processOnce(ctx)
	require.Len(t, sender.ids, 1)
	require.EqualValues(t, 1, countByStatus(ctx, t, repo, domain.DeliveryStatusSent))
}

func TestGenericUnknownEvidencePreventsJoinedConflictReissue(t *testing.T) {
	for _, unknown := range []error{
		sendoutcome.ErrHandoffOutcomeUnknown, context.DeadlineExceeded, context.Canceled,
		&iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_OUTCOME_UNKNOWN"}`},
		errors.Join(sendoutcome.ErrHandoffFailed, &iris.TransportError{Op: "post", Err: io.ErrUnexpectedEOF}),
	} {
		t.Run(unknown.Error(), func(t *testing.T) {
			repo := testRepository(t)
			ctx := t.Context()
			require.NoError(t, repo.Enqueue(ctx, domain.DeliveryKindMemberNewsWeekly, "joined-conflict", "room", "source"))

			sender := &preparedSenderSpy{body: "pinned", route: testPreparedText, err: errors.Join(&iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`}, unknown)}
			d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
			d.processOnce(ctx)
			d.processOnce(ctx)
			require.Len(t, sender.ids, 1)
			require.EqualValues(t, 1, countByStatus(ctx, t, repo, deliveryStatusQuarantined))

			var generation int

			require.NoError(t, repo.pool.QueryRow(ctx, `SELECT (payload->'request'->>'generation')::int FROM notification_delivery_outbox WHERE period_key='joined-conflict'`).Scan(&generation))
			require.Zero(t, generation)
		})
	}
}
