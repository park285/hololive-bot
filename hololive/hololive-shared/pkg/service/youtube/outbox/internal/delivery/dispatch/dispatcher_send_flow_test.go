package dispatch

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatchstate"
	"github.com/park285/iris-client-go/iris"
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
