package alarmdispatch

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
)

type alarmAttemptSender struct {
	alarmDispatchRunnerTestSender

	tracker     *workercontract.ExecutorTracker
	panicValue  bool
	sawInFlight bool
}

func (s *alarmAttemptSender) SendPreparedMessage(ctx context.Context, room, body, route, id string) (string, error) {
	s.sawInFlight = s.tracker.Snapshot(time.Now()).InFlight == 1
	if s.panicValue {
		panic("provider panic")
	}

	return s.alarmDispatchRunnerTestSender.SendPreparedMessage(ctx, room, body, route, id)
}

func TestAlarmProviderAttemptsRecordProviderResultBeforePersistence(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		want   workercontract.AttemptOutcome
		panics bool
	}{
		{name: "success", want: workercontract.AttemptSuccess},
		{name: "failed", err: errors.New("provider failed"), want: workercontract.AttemptFailed},
		{name: "unknown", err: sendoutcome.ErrAdmissionOutcomeUnknown, want: workercontract.AttemptOutcomeUnknown},
		{name: "timeout", err: context.DeadlineExceeded, want: workercontract.AttemptTimeout},
		{name: "canceled", err: context.Canceled, want: workercontract.AttemptCanceled},
		{name: "panic", panics: true, want: workercontract.AttemptPanic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker := workercontract.NewExecutorTracker()
			totals := &workercontract.Counters{}
			sender := &alarmAttemptSender{tracker: tracker, panicValue: tc.panics, messageErr: tc.err}
			consumer := &alarmDispatchRunnerTestConsumer{}
			runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, workerTracker: tracker, workerTotals: totals}
			group := alarmDispatchGroup{roomID: testAlarmRoomID, envelopes: withAlarmDispatchTestSendUnitIdentity([]dispatchoutbox.AlarmQueueEnvelope{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)})}
			request := &dispatchoutbox.SendRequest{RoomID: testAlarmRoomID, Body: "body", Route: dispatchoutbox.SendRouteText, ClientRequestID: group.envelopes[0].ClientRequestID}
			call := func() { require.NoError(t, runner.dispatchPreparedMessageGroup(t.Context(), group, request)) }

			if tc.panics {
				require.Panics(t, call)
			} else {
				call()
			}

			require.True(t, sender.sawInFlight)
			require.Zero(t, tracker.Snapshot(time.Now()).InFlight)

			expected := &workercontract.Counters{}
			expected.RecordAttempt(tc.want)
			require.Equal(t, expected.Snapshot(), totals.Snapshot())
		})
	}
}

func TestAlarmProviderAttemptOutcomeByError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want workercontract.AttemptOutcome
	}{
		{name: "unclassified", err: errors.New("provider failed"), want: workercontract.AttemptFailed},
		{name: "server error", err: &iris.HTTPError{StatusCode: http.StatusServiceUnavailable, URL: testIrisReplyPath}, want: workercontract.AttemptFailed},
		{name: "known unsent transport", err: &iris.TransportError{Op: "post", Err: &net.OpError{Op: "dial", Err: errors.New("refused")}}, want: workercontract.AttemptFailed},
		{name: "response lost", err: &iris.TransportError{Op: "post", Err: io.ErrUnexpectedEOF}, want: workercontract.AttemptOutcomeUnknown},
		{name: "unknown with deadline", err: errors.Join(sendoutcome.ErrAdmissionOutcomeUnknown, context.DeadlineExceeded), want: workercontract.AttemptOutcomeUnknown},
		{name: "unknown with cancel", err: errors.Join(sendoutcome.ErrAdmissionOutcomeUnknown, context.Canceled), want: workercontract.AttemptOutcomeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			totals := &workercontract.Counters{}
			runner := Runner{sender: &alarmDispatchRunnerTestSender{messageErr: tc.err}, workerTracker: workercontract.NewExecutorTracker(), workerTotals: totals}
			request := &dispatchoutbox.SendRequest{RoomID: testAlarmRoomID, Body: "outcome body", Route: dispatchoutbox.SendRouteText, ClientRequestID: "alarm:outcome"}

			_, err := runner.sendPreparedRequest(t.Context(), request)
			require.ErrorIs(t, err, tc.err)

			expected := &workercontract.Counters{}
			expected.RecordAttempt(tc.want)
			require.Equal(t, expected.Snapshot(), totals.Snapshot())
		})
	}
}

func TestAlarmProviderAttemptsCountGroupsAndSkipPreparation(t *testing.T) {
	consumer := &alarmDispatchRunnerTestConsumer{}
	totals := &workercontract.Counters{}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, workerTotals: totals}
	envelopes := withAlarmDispatchTestSendUnitIdentity([]dispatchoutbox.AlarmQueueEnvelope{alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)})
	group := alarmDispatchGroup{envelopes: envelopes}
	request := &dispatchoutbox.SendRequest{Body: "body", RoomID: testAlarmRoomID, Route: dispatchoutbox.SendRouteText, ClientRequestID: envelopes[0].ClientRequestID}

	consumer.markSendingErr = errors.New("prepare failed")

	require.NoError(t, runner.dispatchPreparedMessageGroup(t.Context(), group, request))
	require.Equal(t, (&workercontract.Counters{}).Snapshot(), totals.Snapshot())

	consumer.markSendingErr = nil

	for range 2 {
		require.NoError(t, runner.dispatchPreparedMessageGroup(t.Context(), group, request))
	}

	expected := &workercontract.Counters{}
	expected.RecordAttempt(workercontract.AttemptSuccess)
	expected.RecordAttempt(workercontract.AttemptSuccess)
	require.Equal(t, expected.Snapshot(), totals.Snapshot())

	consumer.markDispatchedErr = errors.New("DB commit failed")

	require.Error(t, runner.dispatchPreparedMessageGroup(t.Context(), group, request))
	expected.RecordAttempt(workercontract.AttemptSuccess)
	require.Equal(t, expected.Snapshot(), totals.Snapshot())
}
