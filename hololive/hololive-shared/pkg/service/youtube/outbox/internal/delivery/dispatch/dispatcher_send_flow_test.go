package dispatch

import (
	"errors"
	"fmt"
	"testing"

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
