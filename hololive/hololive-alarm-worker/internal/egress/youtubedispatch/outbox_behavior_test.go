package youtubedispatch_test

import (
	"testing"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
)

func TestErrDeliveryDedupeKeyRequiredMatchesInternalSentinel(t *testing.T) {
	if got, want := youtubedispatch.ErrDeliveryDedupeKeyRequired.Error(), "delivery dedupe key is required"; got != want {
		t.Fatalf("sentinel message = %q, want %q", got, want)
	}
}
