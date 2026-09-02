package livestatus

import (
	"errors"
	"testing"
)

func TestDeferredErrorMatchesSentinel(t *testing.T) {
	cause := errors.New("cap reached")
	err := NewDeferred(DeferredReasonPerCycleCap, "UCtest", cause)

	if !errors.Is(err, ErrDeferred) {
		t.Fatal("errors.Is(..., ErrDeferred) = false")
	}

	if !IsDeferred(err) {
		t.Fatal("IsDeferred = false")
	}

	if got := ReasonOf(err); got != DeferredReasonPerCycleCap {
		t.Fatalf("ReasonOf = %q, want %q", got, DeferredReasonPerCycleCap)
	}

	if !errors.Is(err, cause) {
		t.Fatal("wrapped cause was not preserved")
	}
}
