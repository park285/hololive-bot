package collection

import (
	"testing"
	"time"
)

func TestRetryAtScheduleNormalizesUTCWithoutChangingInstant(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 2, 12, 0, 0, 123456789, time.FixedZone("KST", 9*60*60))

	schedule, err := NewRetryAtSchedule(at)
	if err != nil {
		t.Fatal(err)
	}

	if got := schedule.At(); got.Location() != time.UTC || !got.Equal(at) {
		t.Fatalf("retry timestamp = %s, want UTC instant %s", got, at.UTC())
	}
}

func TestRetryAtScheduleRejectsEmptyTimestamp(t *testing.T) {
	t.Parallel()

	if _, err := NewRetryAtSchedule(time.Time{}); err == nil {
		t.Fatal("zero retry timestamp must fail")
	}

	if err := (RetrySchedule{}).Validate(); err == nil {
		t.Fatal("empty retry schedule must fail")
	}
}
