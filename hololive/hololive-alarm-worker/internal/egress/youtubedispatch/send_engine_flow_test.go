package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
	"github.com/park285/iris-client-go/v2/iris"
)

func TestDeliveryFailureReason_ClassifiesIrisSentinels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"auth", fmt.Errorf("wrap: %w", &iris.HTTPError{StatusCode: 401}), "auth"},
		{"rate-limited", fmt.Errorf("wrap: %w", &iris.HTTPError{StatusCode: 429}), "rate-limited"},
		{"transport", fmt.Errorf("wrap: %w", &iris.TransportError{Op: "dial", Err: errors.New("conn refused")}), "transport"},
		{"http-permanent", fmt.Errorf("wrap: %w", &iris.HTTPError{StatusCode: 400}), "http-permanent"},
		{"dedupe-key", ErrDeliveryDedupeKeyRequired, "dedupe key"},
		{"generic", errors.New("boom"), "send message"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := deliveryFailureReason(tc.err); got != tc.want {
				t.Fatalf("deliveryFailureReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDeliveryRetryAfterExtractsHTTPErrorHint(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("wrap: %w", &iris.HTTPError{StatusCode: 429, RetryAfter: 12 * time.Second})

	if got := deliveryRetryAfter(err); got != 12*time.Second {
		t.Fatalf("deliveryRetryAfter() = %s, want 12s", got)
	}
}

func TestDeliveryRetryAfterClampsExcessiveHTTPRetryAfter(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("wrap: %w", &iris.HTTPError{StatusCode: 429, RetryAfter: 24 * time.Hour})

	if got := deliveryRetryAfter(err); got != maxDeliveryRetryAfter {
		t.Fatalf("deliveryRetryAfter() = %s, want clamp %s", got, maxDeliveryRetryAfter)
	}
}

func TestMetricsRecorderRecordDeliveryFailureStoresLongestRetryAfter(t *testing.T) {
	t.Parallel()

	result := &dispatchstate.DispatchResult{}
	recorder := &MetricsRecorder{}
	var mu sync.Mutex

	recorder.recordDeliveryFailureWithRetryAfter(result, &mu, "rate-limited", 10, 100, 2*time.Second)
	recorder.recordDeliveryFailureWithRetryAfter(result, &mu, "rate-limited", 11, 101, time.Second)

	if got := result.FailureRetryAfter["rate-limited"]; got != 2*time.Second {
		t.Fatalf("FailureRetryAfter[rate-limited] = %s, want 2s", got)
	}
	if got := result.FailureBuckets["rate-limited"]; len(got) != 2 || got[0] != 10 || got[1] != 11 {
		t.Fatalf("FailureBuckets[rate-limited] = %#v, want [10 11]", got)
	}
}

func TestDeliverySendOutcomeUnknown_Classification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"send-timeout", fmt.Errorf("wrap: %w", errDeliverySendTimeout), true},
		{"deadline", fmt.Errorf("wrap: %w", context.DeadlineExceeded), true},
		{"canceled", context.Canceled, true},
		{"transport-post", fmt.Errorf("wrap: %w", &iris.TransportError{Op: "post", Err: io.ErrUnexpectedEOF}), true},
		{"transport-dial", &iris.TransportError{Op: "post", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}, false},
		{"transport-dns", &iris.TransportError{Op: "post", Err: &net.DNSError{Err: "no such host", IsNotFound: true}}, false},
		{"http-outcome-unknown", &iris.HTTPError{StatusCode: 409, Body: fmt.Sprintf(`{"code":%q}`, iris.HTTPErrorCodeClientRequestIDOutcomeUnknown)}, true},
		{"http-rate-limited", &iris.HTTPError{StatusCode: 429}, false},
		{"http-server-error", &iris.HTTPError{StatusCode: 500}, false},
		{"http-permanent", &iris.HTTPError{StatusCode: 400}, false},
		{"generic", errors.New("boom"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := deliverySendOutcomeUnknown(tc.err); got != tc.want {
				t.Fatalf("deliverySendOutcomeUnknown(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type flowTestSender struct {
	calls  atomic.Int32
	err    error
	onCall func()
}

func (s *flowTestSender) SendMessage(ctx context.Context, _, _ string) error {
	s.calls.Add(1)
	if s.onCall != nil {
		s.onCall()
	}
	if s.err != nil {
		return s.err
	}
	<-ctx.Done()
	return ctx.Err()
}

func newFlowTestSendEngine(sender *flowTestSender, timeout time.Duration) *SendEngine {
	return newSendEngine(sender, &MessageFormatter{}, slog.New(slog.NewTextHandler(io.Discard, nil)), &dispatchstate.Config{
		DeliverySendTimeout: timeout,
	}, nil, nil, nil)
}

func flowTestSendRequest(roomID string) deliverySendRequest {
	return deliverySendRequest{
		roomID:     roomID,
		message:    "hello",
		dedupeKeys: []string{"youtube-notification:NEW_SHORT:" + roomID},
	}
}

func TestSendDeliveryMessageTimeoutIsOutcomeUnknown(t *testing.T) {
	t.Parallel()

	sender := &flowTestSender{}
	engine := newFlowTestSendEngine(sender, 5*time.Millisecond)

	err := engine.sendDeliveryMessage(context.Background(), flowTestSendRequest("room-timeout"))

	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want timeout")
	}
	if !errors.Is(err, errDeliverySendOutcomeUnknown) {
		t.Fatalf("sendDeliveryMessage() error = %v, want outcome-unknown sentinel", err)
	}
	if !errors.Is(err, errDeliverySendTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sendDeliveryMessage() error = %v, want timeout chain preserved", err)
	}
	if shouldFallbackGroupedSend(err) {
		t.Fatal("shouldFallbackGroupedSend() = true for outcome-unknown timeout, want false")
	}
}

func TestSendDeliveryMessageCanceledBeforeRequestStaysRetryable(t *testing.T) {
	t.Parallel()

	sender := &flowTestSender{}
	engine := newFlowTestSendEngine(sender, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := engine.sendDeliveryMessage(ctx, flowTestSendRequest("room-pre-send-cancel"))

	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want pre-send cancellation")
	}
	if errors.Is(err, errDeliverySendOutcomeUnknown) {
		t.Fatalf("sendDeliveryMessage() error = %v, want retryable (not outcome-unknown)", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sendDeliveryMessage() error = %v, want context.Canceled", err)
	}
	if got := sender.calls.Load(); got != 0 {
		t.Fatalf("sender calls = %d, want 0 (request must not leave the client)", got)
	}
	if reason := deliveryFailureReason(err); reason != deliveryReasonSendMessage || deliveryFailureReasonIsPermanent(reason) {
		t.Fatalf("deliveryFailureReason() = %q, want retryable %q", reason, deliveryReasonSendMessage)
	}
}

func TestSendDeliveryMessageCanceledDuringRequestIsOutcomeUnknown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sender := &flowTestSender{onCall: cancel}
	engine := newFlowTestSendEngine(sender, time.Second)

	err := engine.sendDeliveryMessage(ctx, flowTestSendRequest("room-in-flight-cancel"))

	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want cancellation")
	}
	if !errors.Is(err, errDeliverySendOutcomeUnknown) || !errors.Is(err, context.Canceled) {
		t.Fatalf("sendDeliveryMessage() error = %v, want outcome-unknown with context.Canceled", err)
	}
	if got := sender.calls.Load(); got != 1 {
		t.Fatalf("sender calls = %d, want 1", got)
	}
}

func TestSendDeliveryMessageDialFailureStaysRetryable(t *testing.T) {
	t.Parallel()

	sender := &flowTestSender{err: &iris.TransportError{Op: "post", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}}
	engine := newFlowTestSendEngine(sender, time.Second)

	err := engine.sendDeliveryMessage(context.Background(), flowTestSendRequest("room-dial"))

	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want dial failure")
	}
	if errors.Is(err, errDeliverySendOutcomeUnknown) {
		t.Fatalf("sendDeliveryMessage() error = %v, want retryable (request never left the client)", err)
	}
	if reason := deliveryFailureReason(err); reason != deliveryReasonTransport || deliveryFailureReasonIsPermanent(reason) {
		t.Fatalf("deliveryFailureReason() = %q, want retryable %q", reason, deliveryReasonTransport)
	}
}

func TestSendDeliveryMessagePostSendTransportFailureIsOutcomeUnknown(t *testing.T) {
	t.Parallel()

	sender := &flowTestSender{err: &iris.TransportError{Op: "post", Err: io.ErrUnexpectedEOF}}
	engine := newFlowTestSendEngine(sender, time.Second)

	err := engine.sendDeliveryMessage(context.Background(), flowTestSendRequest("room-eof"))

	if err == nil {
		t.Fatal("sendDeliveryMessage() error = nil, want transport failure")
	}
	if !errors.Is(err, errDeliverySendOutcomeUnknown) || !errors.Is(err, iris.ErrTransport) {
		t.Fatalf("sendDeliveryMessage() error = %v, want outcome-unknown transport failure", err)
	}
}

func TestShouldFallbackGroupedSendSkipsOutcomeUnknown(t *testing.T) {
	t.Parallel()

	permanent := &iris.HTTPError{StatusCode: 400}
	if !shouldFallbackGroupedSend(fmt.Errorf("wrap: %w", permanent)) {
		t.Fatal("shouldFallbackGroupedSend() = false for permanent HTTP error, want true")
	}
	if shouldFallbackGroupedSend(fmt.Errorf("wrap: %w", errors.Join(errDeliverySendOutcomeUnknown, permanent))) {
		t.Fatal("shouldFallbackGroupedSend() = true for outcome-unknown permanent error, want false")
	}
}
