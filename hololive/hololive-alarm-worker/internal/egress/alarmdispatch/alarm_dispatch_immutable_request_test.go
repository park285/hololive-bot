package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"testing"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type immutableRequestSender struct {
	alarmDispatchRunnerTestSender

	route  string
	routes []string
}

func (s *immutableRequestSender) PrepareMessageRequest(_ context.Context, _, message string) (string, string, error) {
	return message, s.route, nil
}

func (s *immutableRequestSender) SendPreparedMessage(ctx context.Context, room, body, route, id string) error {
	s.routes = append(s.routes, route)
	return s.SendMessageWithClientRequestID(ctx, room, body, id)
}

func TestAlarmRequestReplaysPinnedTemplateRouteAfterRestart(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	repo := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.Channel.ID = "request-channel"

	_, err := repo.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)

	consumer, err := dispatchoutbox.NewConsumer(repo, requestClaimReleaser{}, nil, dispatchoutbox.WithWorkerID("pin-owner"))
	require.NoError(t, err)

	sender := &immutableRequestSender{route: dispatchoutbox.SendRouteMarkdown, messageErr: context.DeadlineExceeded}
	runner := Runner{consumer: consumer, sender: sender, renderer: template.NewRenderer(pool, slog.Default()), maxBatch: 10}

	_, err = runner.runOnce(ctx)
	require.NoError(t, err)
	require.Len(t, sender.messages, 1)

	_, err = pool.Exec(ctx, `UPDATE notification_templates SET body='CHANGED TEMPLATE'; UPDATE alarm_dispatch_deliveries SET next_attempt_at=NOW()`)
	require.NoError(t, err)

	consumer, err = dispatchoutbox.NewConsumer(dispatchoutbox.NewPgxRepositoryFromPool(pool, nil), requestClaimReleaser{}, nil, dispatchoutbox.WithWorkerID("restarted-owner"))
	require.NoError(t, err)

	sender.route = dispatchoutbox.SendRouteText
	sender.messageErr = nil

	restarted := Runner{consumer: consumer, sender: sender, maxBatch: 10, shortLinkBaseURL: "https://changed.example"}

	_, err = restarted.runOnce(ctx)
	require.NoError(t, err)
	require.Len(t, sender.messages, 2)
	require.Equal(t, sender.messages[0], sender.messages[1])
	require.Equal(t, sender.clientRequestIDs[0], sender.clientRequestIDs[1])
	require.Equal(t, []string{dispatchoutbox.SendRouteMarkdown, dispatchoutbox.SendRouteMarkdown}, sender.routes)
}

func TestAlarmRequestOnlyReissuesConfirmedPreHandoffFailure(t *testing.T) {
	for _, code := range []string{iris.HTTPErrorCodeClientRequestIDFailed, iris.HTTPErrorCodeClientRequestIDPayloadMismatch, iris.HTTPErrorCodeClientRequestIDOutcomeUnknown, iris.HTTPErrorCodeClientRequestIDAlreadyExists, ""} {
		t.Run(code, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			ctx := t.Context()
			repo := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
			envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

			envelope.Notification.Channel.ID = "request-channel"

			_, err := repo.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
			require.NoError(t, err)

			consumer, err := dispatchoutbox.NewConsumer(repo, requestClaimReleaser{}, nil, dispatchoutbox.WithWorkerID("reissue-owner"))
			require.NoError(t, err)

			sender := &immutableRequestSender{route: dispatchoutbox.SendRouteText, messageErr: &iris.HTTPError{StatusCode: http.StatusConflict, Body: fmt.Sprintf(`{"code":%q}`, code)}}
			runner := Runner{consumer: consumer, sender: sender, renderer: template.NewRenderer(pool, slog.Default()), maxBatch: 10}
			attempts := 1

			if code == iris.HTTPErrorCodeClientRequestIDFailed {
				attempts = 3
			}

			for range attempts {
				_, err = runner.runOnce(ctx)
				require.NoError(t, err)

				_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET next_attempt_at=NOW() WHERE status='retry'`)
				require.NoError(t, err)
			}

			require.Len(t, sender.messages, attempts)

			if attempts == 3 {
				require.Equal(t, sender.clientRequestIDs[0]+":r1", sender.clientRequestIDs[1])
				require.Equal(t, sender.clientRequestIDs[0]+":r2", sender.clientRequestIDs[2])
				require.Equal(t, sender.messages[0], sender.messages[2])
			}

			var status string

			require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM alarm_dispatch_deliveries`).Scan(&status))
			require.Equal(t, "quarantined", status)
		})
	}
}

func TestAlarmRequestLegacyAttemptQuarantinesBeforeProvider(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	repo := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.Channel.ID = "request-channel"

	_, err := repo.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET sending_started_at=NOW(),attempt_count=1`)
	require.NoError(t, err)

	consumer, err := dispatchoutbox.NewConsumer(repo, requestClaimReleaser{}, nil, dispatchoutbox.WithWorkerID("legacy-owner"))
	require.NoError(t, err)

	sender := &immutableRequestSender{route: dispatchoutbox.SendRouteText}
	runner := Runner{consumer: consumer, sender: sender, maxBatch: 10}

	_, err = runner.runOnce(ctx)
	require.NoError(t, err)
	require.Empty(t, sender.messages)

	var status string

	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM alarm_dispatch_deliveries`).Scan(&status))
	require.Equal(t, "quarantined", status)
}

func TestAlarmRequestPinnedErrorDoesNotPermitAmbiguousRetryWithoutRequest(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{}
	runner := Runner{consumer: consumer}
	group := alarmDispatchGroup{envelopes: withAlarmDispatchTestSendUnitIdentity([]domain.AlarmQueueEnvelope{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)})}
	require.NoError(t, runner.persistPostSendingFailure(t.Context(), group, context.DeadlineExceeded))
	require.Len(t, consumer.quarantined, 1)
	require.Empty(t, consumer.scheduledSendingRetry)
}

type requestClaimReleaser struct{}

func (requestClaimReleaser) DelMany(context.Context, []string) (int64, error) { return 0, nil }

func TestAlarmRequestUnknownEvidencePreventsReissueOfJoinedConflict(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{}
	runner := Runner{consumer: consumer}
	envelopes := withAlarmDispatchTestSendUnitIdentity([]domain.AlarmQueueEnvelope{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)})
	group := alarmDispatchGroup{envelopes: envelopes, request: &dispatchoutbox.SendRequest{ClientRequestID: envelopes[0].ClientRequestID}}
	cause := errors.Join(sendoutcome.ErrHandoffOutcomeUnknown, &iris.HTTPError{StatusCode: http.StatusConflict, Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`})
	require.NoError(t, runner.persistPostSendingFailure(t.Context(), group, cause))
	require.Zero(t, consumer.reissueCalls)
	require.Len(t, consumer.quarantined, 1)
}

func TestAlarmRequestTimeoutDoesNotReissueJoinedPreHandoffConflict(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{}
	runner := Runner{consumer: consumer}
	envelopes := withAlarmDispatchTestSendUnitIdentity([]domain.AlarmQueueEnvelope{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)})
	group := alarmDispatchGroup{envelopes: envelopes, request: &dispatchoutbox.SendRequest{ClientRequestID: envelopes[0].ClientRequestID}}
	cause := errors.Join(context.DeadlineExceeded, &iris.HTTPError{StatusCode: http.StatusConflict, Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`})
	require.NoError(t, runner.persistPostSendingFailure(t.Context(), group, cause))
	require.Zero(t, consumer.reissueCalls)
	require.Len(t, consumer.scheduledSendingRetry, 1)
	require.Empty(t, consumer.quarantined)
}
