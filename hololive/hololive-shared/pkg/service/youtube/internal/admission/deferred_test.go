package admission

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDeferredError_ErrorIdentityAndRetryAfter(t *testing.T) {
	retryAfter := 3 * time.Second
	err := fmt.Errorf("wrapped: %w", NewDeferredError(
		"youtube_scraper_rate_limit",
		"youtube:producer:channel:UC123:community",
		"local_interval",
		retryAfter,
		nil,
	))

	if !IsDeferred(err) {
		t.Fatalf("IsDeferred() = false, want true")
	}
	if !errors.Is(err, ErrDeferred) {
		t.Fatalf("errors.Is(err, ErrDeferred) = false, want true")
	}
	gotRetryAfter, ok := RetryAfter(err)
	if !ok || gotRetryAfter != retryAfter {
		t.Fatalf("RetryAfter() = (%s, %v), want (%s, true)", gotRetryAfter, ok, retryAfter)
	}
	if !strings.Contains(err.Error(), "reason=local_interval") {
		t.Fatalf("error message does not include reason: %q", err.Error())
	}
}

func TestIsDeferred_DirectSentinel(t *testing.T) {
	if !IsDeferred(ErrDeferred) {
		t.Fatalf("direct ErrDeferred was not recognized")
	}
	if retryAfter, ok := RetryAfter(ErrDeferred); ok || retryAfter != 0 {
		t.Fatalf("RetryAfter(ErrDeferred) = (%s, %v), want (0, false)", retryAfter, ok)
	}
}
