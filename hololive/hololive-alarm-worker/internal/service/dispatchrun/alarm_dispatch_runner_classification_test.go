package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
)

func TestIsAlarmDispatchRetryablePostSendFailure_TypedHTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "typed 502 direct",
			err:  &iris.HTTPError{StatusCode: 502, URL: "/karing/content-list"},
			want: true,
		},
		{
			name: "typed 503 direct",
			err:  &iris.HTTPError{StatusCode: 503, URL: "/karing/content-list"},
			want: true,
		},
		{
			name: "typed 429 direct",
			err:  &iris.HTTPError{StatusCode: 429, URL: "/karing/content-list"},
			want: true,
		},
		{
			name: "typed 502 wrapped through fmt.Errorf",
			err:  fmt.Errorf("iris send karing content list: %w", fmt.Errorf("send iris karing content list: %w", &iris.HTTPError{StatusCode: 502, URL: "/karing/content-list"})),
			want: true,
		},
		{
			name: "typed 503 wrapped through fmt.Errorf",
			err:  fmt.Errorf("iris send karing content list: %w", &iris.HTTPError{StatusCode: 503, URL: "/karing/content-list"}),
			want: true,
		},
		{
			name: "typed 502 on different URL should also be retryable",
			err:  &iris.HTTPError{StatusCode: 502, URL: "/some-other-endpoint"},
			want: true,
		},
		{
			name: "typed 503 with empty URL should be retryable",
			err:  &iris.HTTPError{StatusCode: 503},
			want: true,
		},
		{
			name: "typed 500 not retryable",
			err:  &iris.HTTPError{StatusCode: 500, URL: "/karing/content-list"},
			want: false,
		},
		{
			name: "typed 504 not retryable",
			err:  &iris.HTTPError{StatusCode: 504, URL: "/karing/content-list"},
			want: false,
		},
		{
			name: "typed 401 not retryable",
			err:  &iris.HTTPError{StatusCode: 401, URL: "/karing/content-list"},
			want: false,
		},
		{
			name: "typed 403 not retryable",
			err:  &iris.HTTPError{StatusCode: 403, URL: "/karing/content-list"},
			want: false,
		},
		{
			name: "typed 400 not retryable",
			err:  &iris.HTTPError{StatusCode: 400, URL: "/karing/content-list"},
			want: false,
		},
		{
			name: "transport error direct",
			err:  &iris.TransportError{Op: "post", URL: "/karing/content-list", Err: errors.New("connection refused")},
			want: true,
		},
		{
			name: "transport error wrapped through fmt.Errorf",
			err:  fmt.Errorf("send iris karing content list: %w", &iris.TransportError{Op: "post", URL: "/karing/content-list", Err: errors.New("connection reset by peer")}),
			want: true,
		},
		{
			name: "transport error wrapping deadline exceeded",
			err:  &iris.TransportError{Op: "post", URL: "/karing/content-list", Err: context.DeadlineExceeded},
			want: true,
		},
		{
			name: "bare deadline exceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "deadline exceeded wrapped through fmt.Errorf",
			err:  fmt.Errorf("send alarm dispatch message: %w", context.DeadlineExceeded),
			want: true,
		},
		{
			name: "context canceled stays non-retryable",
			err:  context.Canceled,
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("something else failed"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isAlarmDispatchRetryablePostSendFailure(tc.err)
			if got != tc.want {
				t.Errorf("isAlarmDispatchRetryablePostSendFailure(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestAlarmDispatchMaxAttemptsForCause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "retryable 503 gets the extended budget",
			err:  &iris.HTTPError{StatusCode: 503},
			want: alarmDispatchRetryableMaxAttempts,
		},
		{
			name: "transport error gets the extended budget",
			err:  &iris.TransportError{Op: "post", URL: "/karing/content-list", Err: errors.New("connection refused")},
			want: alarmDispatchRetryableMaxAttempts,
		},
		{
			name: "deadline exceeded gets the extended budget",
			err:  context.DeadlineExceeded,
			want: alarmDispatchRetryableMaxAttempts,
		},
		{
			name: "non-retryable 500 keeps the base budget",
			err:  &iris.HTTPError{StatusCode: 500},
			want: alarmDispatchMaxAttempts,
		},
		{
			name: "unrelated error keeps the base budget",
			err:  errors.New("render failed"),
			want: alarmDispatchMaxAttempts,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := alarmDispatchMaxAttemptsForCause(tc.err); got != tc.want {
				t.Errorf("alarmDispatchMaxAttemptsForCause(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestAlarmDispatchRetryableBudgetOutlivesIrisRestart(t *testing.T) {
	t.Parallel()

	if alarmDispatchRetryableMaxAttempts <= alarmDispatchMaxAttempts {
		t.Fatalf("retryable budget %d must exceed base budget %d",
			alarmDispatchRetryableMaxAttempts, alarmDispatchMaxAttempts)
	}

	horizon := time.Duration(0)
	for attempt := 1; attempt < alarmDispatchRetryableMaxAttempts; attempt++ {
		horizon += time.Duration(attempt) * 5 * time.Second
	}
	if horizon < 60*time.Second {
		t.Errorf("retry horizon %s must outlast a 30-60s Iris restart", horizon)
	}
}
