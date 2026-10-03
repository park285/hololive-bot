package sourceobservation

import "testing"

func TestPublishedObservationOrdinalDefaultsAndConstructor(t *testing.T) {
	t.Parallel()

	var zero PublishedObservation

	if zero.Ordinal != 0 {
		t.Fatalf("zero ordinal = %d", zero.Ordinal)
	}

	got := NewPublishedObservation(9, PublishInserted, 3)
	if got.ObservationID != 9 || got.Outcome != PublishInserted || got.Ordinal != 3 {
		t.Fatalf("NewPublishedObservation = %#v", got)
	}
}
