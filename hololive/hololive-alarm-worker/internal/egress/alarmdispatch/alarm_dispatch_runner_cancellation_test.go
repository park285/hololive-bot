package alarmdispatch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
)

type alarmDispatchCancellationConsumer struct {
	Consumer

	afterMark       func(context.Context) error
	requeueErr      error
	markCtxErr      error
	requeueCtxErr   error
	requeueDeadline bool
}

func (c *alarmDispatchCancellationConsumer) MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.markCtxErr = ctx.Err()
	if err := c.Consumer.MarkSending(ctx, envelopes); err != nil {
		return fmt.Errorf("mark sending: %w", err)
	}

	if c.afterMark != nil {
		if err := c.afterMark(ctx); err != nil {
			return fmt.Errorf("after mark sending: %w", err)
		}
	}

	return nil
}

func (c *alarmDispatchCancellationConsumer) RequeuePreSend(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	c.requeueCtxErr = ctx.Err()
	_, c.requeueDeadline = ctx.Deadline()

	if c.requeueErr != nil {
		return c.requeueErr
	}

	if err := c.Consumer.RequeuePreSend(ctx, envelopes); err != nil {
		return fmt.Errorf("requeue pre send: %w", err)
	}

	return nil
}

type alarmDispatchCancellationIrisClient struct {
	handoffs int
	onSend   func(context.Context) error
}

func (c *alarmDispatchCancellationIrisClient) SendMessage(ctx context.Context, _, _ string, _ ...iris.SendOption) error {
	c.handoffs++
	if c.onSend != nil {
		return c.onSend(ctx)
	}

	return nil
}

func (*alarmDispatchCancellationIrisClient) SendMarkdown(context.Context, string, string, ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	return nil, errors.New("unexpected markdown send")
}

func (*alarmDispatchCancellationIrisClient) GetReplyStatus(context.Context, string) (*iris.ReplyStatusSnapshot, error) {
	return nil, errors.New("unexpected reply status lookup")
}

func alarmDispatchCancellationRequest(attempt int) (alarmDispatchGroup, *dispatchoutbox.SendRequest) {
	envelopes := withAlarmDispatchTestSendUnitIdentity([]domain.AlarmQueueEnvelope{
		alarmDispatchRunnerTestEnvelope(testAlarmRoomID, &domain.AlarmQueueRetryMetadata{Attempt: attempt}),
	})
	group := alarmDispatchGroup{roomID: testAlarmRoomID, envelopes: envelopes}
	request := &dispatchoutbox.SendRequest{Body: "body", RoomID: testAlarmRoomID, Route: dispatchoutbox.SendRouteText, ClientRequestID: envelopes[0].ClientRequestID}

	return group, request
}

func TestAlarmDispatchRunnerCancellationBeforeMarkSending(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	consumer := &alarmDispatchRunnerTestConsumer{}
	client := &alarmDispatchCancellationIrisClient{}
	runner := Runner{consumer: consumer, sender: egress.NewIrisMessageSender(client)}
	group, request := alarmDispatchCancellationRequest(2)

	require.ErrorIs(t, runner.dispatchPreparedMessageGroup(ctx, group, request), context.Canceled)
	require.Zero(t, client.handoffs)
	require.Empty(t, consumer.markSending)
	require.Empty(t, consumer.preSendRequeued)
	require.Empty(t, consumer.quarantined)
}

func TestAlarmDispatchRunnerCancellationDuringMarkSendingRequeuesBeforeHandoff(t *testing.T) {
	for _, attempt := range []int{0, 2} {
		t.Run(fmt.Sprintf("attempt_%d", attempt), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			recorded := &alarmDispatchRunnerTestConsumer{}
			consumer := &alarmDispatchCancellationConsumer{Consumer: recorded, afterMark: func(stateCtx context.Context) error {
				cancel()

				return stateCtx.Err()
			}}
			client := &alarmDispatchCancellationIrisClient{}
			totals := &workercontract.Counters{}
			runner := Runner{consumer: consumer, sender: egress.NewIrisMessageSender(client), workerTotals: totals}
			group, request := alarmDispatchCancellationRequest(attempt)

			require.NoError(t, runner.dispatchPreparedMessageGroup(ctx, group, request))
			require.Zero(t, client.handoffs)
			require.Len(t, recorded.markSending, 1)
			require.Len(t, recorded.preSendRequeued, 1)
			require.Equal(t, attempt, recorded.preSendRequeued[0].Retry.Attempt)
			require.Equal(t, group.envelopes[0].ClientRequestID, recorded.preSendRequeued[0].ClientRequestID)
			require.Equal(t, "canceled", recorded.preSendRequeued[0].Retry.LastErrorCode)
			require.NoError(t, consumer.markCtxErr)
			require.NoError(t, consumer.requeueCtxErr)
			require.True(t, consumer.requeueDeadline)
			require.Empty(t, recorded.quarantined)
			require.Empty(t, recorded.scheduledSendingRetry)
			require.Empty(t, recorded.scheduledRetry)
			require.Empty(t, recorded.markDispatched)
			require.Equal(t, (&workercontract.Counters{}).Snapshot(), totals.Snapshot())
		})
	}
}

func TestAlarmDispatchRunnerCancellationCompensationFailureSurfaces(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	recorded := &alarmDispatchRunnerTestConsumer{}
	stateErr := errors.New("pre-send state update unavailable")
	consumer := &alarmDispatchCancellationConsumer{Consumer: recorded, requeueErr: stateErr, afterMark: func(context.Context) error {
		cancel()

		return nil
	}}
	client := &alarmDispatchCancellationIrisClient{}
	runner := Runner{consumer: consumer, sender: egress.NewIrisMessageSender(client)}
	group, request := alarmDispatchCancellationRequest(2)

	require.ErrorIs(t, runner.dispatchPreparedMessageGroup(ctx, group, request), stateErr)
	require.Zero(t, client.handoffs)
	require.Len(t, recorded.markSending, 1)
	require.NoError(t, consumer.requeueCtxErr)
	require.Empty(t, recorded.quarantined)
	require.Empty(t, recorded.scheduledSendingRetry)
	require.Empty(t, recorded.markDispatched)
}

func TestAlarmDispatchRunnerCancellationAfterHandoffKeepsQuarantine(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown_%t", unknown), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			consumer := &alarmDispatchRunnerContextConsumer{}
			client := &alarmDispatchCancellationIrisClient{onSend: func(sendCtx context.Context) error {
				cancel()

				if unknown {
					return errors.Join(sendoutcome.ErrHandoffOutcomeUnknown, sendCtx.Err())
				}

				return sendCtx.Err()
			}}
			runner := Runner{consumer: consumer, sender: egress.NewIrisMessageSender(client)}
			group, request := alarmDispatchCancellationRequest(2)

			require.NoError(t, runner.dispatchPreparedMessageGroup(ctx, group, request))
			require.Equal(t, 1, client.handoffs)
			require.Len(t, consumer.quarantined, 1)
			require.Equal(t, 2, consumer.quarantined[0].Retry.Attempt)
			require.NoError(t, consumer.quarantineCtxErr)
			require.Empty(t, consumer.preSendRequeued)
			require.Empty(t, consumer.scheduledSendingRetry)
			require.Empty(t, consumer.markDispatched)
		})
	}
}

func TestAlarmDispatchRunnerCancellationCompensationPreservesOwnerAndAttemptFence(t *testing.T) {
	cases := []struct {
		name        string
		owner       string
		attempt     int
		wantPartial bool
	}{
		{name: "same owner and attempt", owner: "cancel-owner", attempt: 2},
		{name: "owner lost", owner: "other-owner", attempt: 2, wantPartial: true},
		{name: "attempt changed", owner: "cancel-owner", attempt: 3, wantPartial: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := dbtest.NewPool(t)
			base, group, request := alarmDispatchCancellationPersistedRequest(t, pool)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			consumer := &alarmDispatchCancellationConsumer{Consumer: base, afterMark: func(stateCtx context.Context) error {
				_, updateErr := pool.Exec(stateCtx, "UPDATE alarm_dispatch_deliveries SET locked_by=$1, attempt_count=$2 WHERE id=$3", tc.owner, tc.attempt, group.envelopes[0].DispatchOutboxID)

				cancel()

				return updateErr
			}}
			client := &alarmDispatchCancellationIrisClient{}
			runner := Runner{consumer: consumer, sender: egress.NewIrisMessageSender(client)}

			err := runner.dispatchPreparedMessageGroup(ctx, group, request)

			if tc.wantPartial {
				partial, ok := errors.AsType[*dispatchoutbox.PartialTransitionError](err)
				require.True(t, ok, "compensation must surface the ownership/attempt fence failure: %v", err)
				require.Equal(t, []int64{group.envelopes[0].DispatchOutboxID}, partial.UnappliedIDs)
			} else {
				require.NoError(t, err)
			}

			require.Zero(t, client.handoffs)
			requireAlarmDispatchCancellationDelivery(t, pool, group.envelopes[0].DispatchOutboxID, tc.owner, tc.attempt, tc.wantPartial)
		})
	}
}

func alarmDispatchCancellationPersistedRequest(t *testing.T, pool *pgxpool.Pool) (*dispatchoutbox.Consumer, alarmDispatchGroup, *dispatchoutbox.SendRequest) {
	t.Helper()

	repo := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	_, err := repo.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)}})
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), "UPDATE alarm_dispatch_deliveries SET attempt_count=2")
	require.NoError(t, err)

	consumer, err := dispatchoutbox.NewConsumer(repo, requestClaimReleaser{}, nil, dispatchoutbox.WithWorkerID("cancel-owner"))
	require.NoError(t, err)

	envelopes, err := consumer.DrainBatch(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)

	request, err := consumer.PinSendRequest(t.Context(), envelopes, dispatchoutbox.SendRequest{Body: "body", Route: dispatchoutbox.SendRouteText, RoomID: testAlarmRoomID})
	require.NoError(t, err)

	return consumer, alarmDispatchGroup{roomID: testAlarmRoomID, envelopes: envelopes}, request
}

func requireAlarmDispatchCancellationDelivery(t *testing.T, pool *pgxpool.Pool, deliveryID int64, wantOwner string, wantAttempt int, wantPartial bool) {
	t.Helper()

	var (
		status, owner, errorCode string
		attempt                  int
		nextAttempt              time.Time
	)

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT status, COALESCE(locked_by,''), attempt_count, COALESCE(last_error_code,''), next_attempt_at FROM alarm_dispatch_deliveries WHERE id=$1", deliveryID).Scan(&status, &owner, &attempt, &errorCode, &nextAttempt))
	require.Equal(t, wantAttempt, attempt)

	if wantPartial {
		require.Equal(t, "sending", status)
		require.Equal(t, wantOwner, owner)
	} else {
		require.Equal(t, "retry", status)
		require.Empty(t, owner)
		require.Equal(t, "canceled", errorCode)
		require.True(t, nextAttempt.After(time.Now()))
	}
}
