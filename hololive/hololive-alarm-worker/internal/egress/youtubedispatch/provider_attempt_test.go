package youtubedispatch

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/testutil"
)

const (
	testProviderAttemptRoom = "room"
	testProviderAttemptBody = "body"
	testTransportOpDial     = "dial"
)

type providerAttemptSender struct {
	tracker    *workercontract.ExecutorTracker
	err        error
	panicValue bool
	seen       bool
}

func (s *providerAttemptSender) SendMessage(context.Context, string, string) error {
	s.seen = s.tracker.Snapshot(time.Now()).InFlight == 1
	if s.panicValue {
		panic("provider panic")
	}

	return s.err
}

func TestProviderAttemptTracksActualResultAndPanic(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		panics  bool
		outcome workercontract.AttemptOutcome
	}{
		{name: "success", outcome: workercontract.AttemptSuccess},
		{name: "failed", err: iris.ErrPermanent, outcome: workercontract.AttemptFailed},
		{name: "unknown", err: sendoutcome.ErrAdmissionOutcomeUnknown, outcome: workercontract.AttemptOutcomeUnknown},
		{name: "timeout", err: context.DeadlineExceeded, outcome: workercontract.AttemptTimeout},
		{name: "canceled", err: context.Canceled, outcome: workercontract.AttemptCanceled},
		{name: "panic", panics: true, outcome: workercontract.AttemptPanic},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker := workercontract.NewExecutorTracker()
			sender := &providerAttemptSender{tracker: tracker, err: tc.err, panicValue: tc.panics}
			d := newTestDispatcherForSend(t, &testSender{})

			d.send.sender = sender

			totals := &workercontract.Counters{}
			d.SetWorkerInstrumentation(tracker, totals)

			var err error

			call := func() {
				_, err = d.send.sendDeliveryMessage(t.Context(), deliverySendRequest{frozen: &store.FrozenRequest{Route: testPreparedTextRoute}, clientRequestID: testClientRequestID, roomID: testProviderAttemptRoom, message: testProviderAttemptBody, dedupeKeys: []string{"one", "two"}})
			}

			if tc.panics {
				require.Panics(t, call)
			} else {
				call()
			}

			require.True(t, sender.seen)
			require.Zero(t, tracker.Snapshot(time.Now()).InFlight)

			expected := &workercontract.Counters{}
			expected.RecordAttempt(tc.outcome)
			require.Equal(t, expected.Snapshot(), totals.Snapshot())

			if !tc.panics {
				require.Equal(t, tc.err == nil, err == nil)
			}
		})
	}
}

func TestGroupedAdmissionConflictsDoNotSplitRequest(t *testing.T) {
	for _, code := range []string{"CLIENT_REQUEST_ID_PAYLOAD_MISMATCH", "CLIENT_REQUEST_ID_ALREADY_EXISTS", "CLIENT_REQUEST_ID_OUTCOME_UNKNOWN", "CLIENT_REQUEST_ID_FAILED", ""} {
		err := testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"`+code+`"}`)
		require.False(t, shouldFallbackGroupedSend(errors.Join(iris.ErrPermanent, err)))
	}
}

func TestReissueWaitsForNextDeliveryAttempt(t *testing.T) {
	sender := &providerAttemptSender{tracker: workercontract.NewExecutorTracker(), err: testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`)}
	d := newTestDispatcherForSend(t, &testSender{})

	d.send.sender = sender
	d.send.config.MaxRetries = 3

	req := deliverySendRequest{clientRequestID: "youtube:reissue", roomID: testProviderAttemptRoom, message: testProviderAttemptBody, dedupeKeys: []string{"key"}, frozen: &store.FrozenRequest{BaseID: "youtube:reissue", RoomID: testProviderAttemptRoom, Message: testProviderAttemptBody, Route: testPreparedTextRoute}}
	err := d.send.sendFrozenDelivery(t.Context(), &deliveryAttempt{request: req})
	require.ErrorIs(t, err, errRequestReissued)
}

func TestReissueRejectsMixedUnknownAndPreHandoffFailure(t *testing.T) {
	sender := &providerAttemptSender{tracker: workercontract.NewExecutorTracker(), err: errors.Join(sendoutcome.ErrAdmissionOutcomeUnknown, testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`))}
	d := newTestDispatcherForSend(t, &testSender{})

	d.send.sender = sender

	req := deliverySendRequest{clientRequestID: "youtube:mixed", roomID: testProviderAttemptRoom, message: testProviderAttemptBody, dedupeKeys: []string{"key"}, frozen: &store.FrozenRequest{BaseID: "youtube:mixed", RoomID: testProviderAttemptRoom, Message: testProviderAttemptBody, Route: testPreparedTextRoute}}
	err := d.send.sendFrozenDelivery(t.Context(), &deliveryAttempt{request: req})
	require.ErrorIs(t, err, errDeliverySendOutcomeUnknown)
	require.NotErrorIs(t, err, errRequestReissued)
}

func TestProviderAttemptSkipsUnsupportedStoredRoute(t *testing.T) {
	tracker := workercontract.NewExecutorTracker()
	sender := &providerAttemptSender{tracker: tracker}
	d := newTestDispatcherForSend(t, &testSender{})

	d.send.sender = sender

	totals := &workercontract.Counters{}
	d.SetWorkerInstrumentation(tracker, totals)

	_, err := d.send.sendDeliveryMessage(t.Context(), deliverySendRequest{roomID: testProviderAttemptRoom, message: testProviderAttemptBody, dedupeKeys: []string{"one"}, frozen: &store.FrozenRequest{Route: "sender"}})
	require.Error(t, err)
	require.False(t, sender.seen)
	require.Zero(t, tracker.Snapshot(time.Now()).InFlight)
	require.Equal(t, (&workercontract.Counters{}).Snapshot(), totals.Snapshot())
}

func (s *providerAttemptSender) PrepareMessageRequest(_ context.Context, _ string, body delivery.MessageVariants) (string, string, error) {
	return body.Plain, testPreparedTextRoute, nil
}

func (s *providerAttemptSender) SendPreparedMessage(ctx context.Context, room, body, _, id string) (string, error) {
	return testAdmission(id, s.SendMessage(ctx, room, body))
}

type outcomeProviderSender struct {
	send func(context.Context) error
}

func (s outcomeProviderSender) PrepareMessageRequest(_ context.Context, _ string, body delivery.MessageVariants) (string, string, error) {
	return body.Plain, testPreparedTextRoute, nil
}

func (s outcomeProviderSender) SendPreparedMessage(ctx context.Context, _, _, _, id string) (string, error) {
	return testAdmission(id, s.send(ctx))
}

func TestProviderAttemptOutcomeByError(t *testing.T) {
	returns := func(err error) func(context.Context) error {
		return func(context.Context) error { return err }
	}
	afterSendTimeout := func(err error) func(context.Context) error {
		return func(ctx context.Context) error {
			<-ctx.Done()

			return err
		}
	}

	cases := []struct {
		name string
		send func(context.Context) error
		want workercontract.AttemptOutcome
	}{
		{name: "unclassified", send: returns(errors.New("provider failed")), want: workercontract.AttemptOutcomeUnknown},
		{name: "server error", send: returns(&iris.HTTPError{StatusCode: http.StatusServiceUnavailable}), want: workercontract.AttemptOutcomeUnknown},
		{name: "pre-handoff conflict", send: returns(testutil.IrisHTTPError(t.Context(), t, 409, `{"code":"CLIENT_REQUEST_ID_FAILED"}`)), want: workercontract.AttemptFailed},
		{name: "known unsent transport", send: returns(&iris.TransportError{Op: testTransportOpPost, Err: &net.OpError{Op: testTransportOpDial, Err: errors.New("refused")}}), want: workercontract.AttemptFailed},
		{name: "response lost", send: returns(&iris.TransportError{Op: testTransportOpPost, Err: io.ErrUnexpectedEOF}), want: workercontract.AttemptOutcomeUnknown},
		{name: "unknown with deadline", send: returns(errors.Join(sendoutcome.ErrAdmissionOutcomeUnknown, context.DeadlineExceeded)), want: workercontract.AttemptOutcomeUnknown},
		{name: "unknown with cancel", send: returns(errors.Join(sendoutcome.ErrAdmissionOutcomeUnknown, context.Canceled)), want: workercontract.AttemptOutcomeUnknown},
		{name: "send timeout with known failure", send: afterSendTimeout(iris.ErrPermanent), want: workercontract.AttemptTimeout},
		{name: "send timeout with unknown evidence", send: afterSendTimeout(sendoutcome.ErrAdmissionOutcomeUnknown), want: workercontract.AttemptOutcomeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				totals := &workercontract.Counters{}
				engine := &SendEngine{
					workerTracker: workercontract.NewExecutorTracker(),
					workerTotals:  totals,
					sender:        outcomeProviderSender{send: tc.send},
					config:        Config{DeliverySendTimeout: time.Second},
				}

				_, err := engine.sendDeliveryMessage(t.Context(), deliverySendRequest{frozen: &store.FrozenRequest{Route: testPreparedTextRoute}, clientRequestID: testClientRequestID, roomID: testProviderAttemptRoom, message: testProviderAttemptBody, dedupeKeys: []string{"one"}})
				require.Error(t, err)

				expected := &workercontract.Counters{}
				expected.RecordAttempt(tc.want)
				require.Equal(t, expected.Snapshot(), totals.Snapshot())
			})
		})
	}
}
