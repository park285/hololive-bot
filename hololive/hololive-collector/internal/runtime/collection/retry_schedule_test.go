package collection

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
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

func TestRetryNotBeforeSchedulePreservesDeadlineAtDatabasePrecision(t *testing.T) {
	t.Parallel()

	for _, nanos := range []int{0, 123456000, 123456789, 999999999} {
		at := time.Date(2026, time.October, 4, 12, 0, 0, nanos, time.FixedZone("KST", 9*60*60))

		schedule, err := NewRetryNotBeforeSchedule(at)
		if err != nil {
			t.Fatal(err)
		}

		if !schedule.IsNotBefore() || schedule.At().Location() != time.UTC || schedule.At().Before(at) ||
			schedule.At().Sub(at) >= time.Microsecond {
			t.Fatalf("not-before = %s, want UTC deadline >= %s rounded by less than 1us", schedule.At(), at)
		}
	}
}

func TestRetryNotBeforeRejectsInvalidTimestampAndKind(t *testing.T) {
	t.Parallel()

	for _, at := range []time.Time{
		{},
		time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC),
	} {
		if _, err := NewRetryNotBeforeSchedule(at); err == nil {
			t.Fatalf("invalid not-before timestamp accepted: %s", at)
		}
	}

	for _, kind := range []retryScheduleKind{0, 255} {
		if err := (RetrySchedule{at: time.Now().UTC(), kind: kind}).Validate(); err == nil {
			t.Fatalf("invalid retry kind accepted: %d", kind)
		}
	}
}

func TestNotBeforeRequiresCooldownDiagnosticAndValidBounds(t *testing.T) {
	t.Parallel()

	schedule, err := NewRetryNotBeforeSchedule(time.Now().Add(10 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	bounds := RetryBounds{Minimum: time.Second, Maximum: 5 * time.Minute}

	for _, tuple := range contract.DeferFailureTuples() {
		diagnostic, diagnosticErr := contract.NewFailureDiagnostic(tuple.Code, tuple.Class, "retry")
		if diagnosticErr != nil {
			t.Fatal(diagnosticErr)
		}

		_, err = NewDeferCollectionInput(diagnostic, bounds, schedule)

		wantValid := tuple.Code == contract.ErrorCooldown && tuple.Class == contract.ClassCooldown

		if (err == nil) != wantValid {
			t.Fatalf("not-before %s/%s error = %v", tuple.Code, tuple.Class, err)
		}
	}

	diagnostic, err := contract.NewFailureDiagnostic(contract.ErrorCooldown, contract.ClassCooldown, "cooldown")
	if err != nil {
		t.Fatal(err)
	}

	for _, invalid := range []RetryBounds{
		{},
		{Minimum: -time.Second, Maximum: time.Minute},
		{Minimum: time.Minute, Maximum: time.Second},
		{Minimum: time.Second, Maximum: time.Hour + time.Millisecond},
		{Minimum: time.Nanosecond, Maximum: time.Minute},
	} {
		if _, err := NewDeferCollectionInput(diagnostic, invalid, schedule); err == nil {
			t.Fatalf("not-before bypassed invalid bounds: %+v", invalid)
		}
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
