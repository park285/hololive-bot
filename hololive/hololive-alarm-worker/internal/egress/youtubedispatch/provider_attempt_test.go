package youtubedispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
)

const (
	testProviderAttemptRoom = "room"
	testProviderAttemptBody = "body"
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
		{name: "unknown", err: sendoutcome.ErrHandoffOutcomeUnknown, outcome: workercontract.AttemptOutcomeUnknown},
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
				err = d.send.sendDeliveryMessage(t.Context(), deliverySendRequest{roomID: testProviderAttemptRoom, message: testProviderAttemptBody, dedupeKeys: []string{"one", "two"}})
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
		err := &iris.HTTPError{StatusCode: 409, Body: `{"code":"` + code + `"}`}
		require.False(t, shouldFallbackGroupedSend(errors.Join(iris.ErrPermanent, err)))
	}
}

func TestReissueWaitsForNextDeliveryAttempt(t *testing.T) {
	sender := &providerAttemptSender{tracker: workercontract.NewExecutorTracker(), err: &iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`}}
	d := newTestDispatcherForSend(t, &testSender{})

	d.send.sender = sender
	d.send.config.MaxRetries = 3

	req := deliverySendRequest{roomID: testProviderAttemptRoom, message: testProviderAttemptBody, dedupeKeys: []string{"key"}, frozen: &store.FrozenRequest{BaseID: "youtube:reissue", RoomID: testProviderAttemptRoom, Message: testProviderAttemptBody, Route: "sender"}}
	err := d.send.sendFrozenDelivery(t.Context(), store.StartedOperation{}, req)
	require.ErrorIs(t, err, errRequestReissued)
}

func TestReissueRejectsMixedUnknownAndPreHandoffFailure(t *testing.T) {
	sender := &providerAttemptSender{tracker: workercontract.NewExecutorTracker(), err: errors.Join(sendoutcome.ErrHandoffOutcomeUnknown, &iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_FAILED"}`})}
	d := newTestDispatcherForSend(t, &testSender{})

	d.send.sender = sender

	req := deliverySendRequest{roomID: testProviderAttemptRoom, message: testProviderAttemptBody, dedupeKeys: []string{"key"}, frozen: &store.FrozenRequest{BaseID: "youtube:mixed", RoomID: testProviderAttemptRoom, Message: testProviderAttemptBody, Route: "sender"}}
	err := d.send.sendFrozenDelivery(t.Context(), store.StartedOperation{}, req)
	require.ErrorIs(t, err, errDeliverySendOutcomeUnknown)
	require.NotErrorIs(t, err, errRequestReissued)
}

func TestProviderAttemptSkipsUnsupportedPreparedSender(t *testing.T) {
	tracker := workercontract.NewExecutorTracker()
	sender := &providerAttemptSender{tracker: tracker}
	d := newTestDispatcherForSend(t, &testSender{})

	d.send.sender = sender

	totals := &workercontract.Counters{}
	d.SetWorkerInstrumentation(tracker, totals)

	err := d.send.sendDeliveryMessage(t.Context(), deliverySendRequest{roomID: testProviderAttemptRoom, message: testProviderAttemptBody, dedupeKeys: []string{"one"}, frozen: &store.FrozenRequest{Route: "text"}})
	require.ErrorContains(t, err, "prepared sender required")
	require.False(t, sender.seen)
	require.Zero(t, tracker.Snapshot(time.Now()).InFlight)
	require.Equal(t, (&workercontract.Counters{}).Snapshot(), totals.Snapshot())
}
