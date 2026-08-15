package content

import (
	"testing"
	"time"
)

func TestReduceCopiesInputBackingStorage(t *testing.T) {
	t.Parallel()
	state := seededState()
	evidence := positiveA()
	decision, err := Reduce(*state, evidence, 0)
	if err != nil {
		t.Fatal(err)
	}

	originalPublished := *evidence.Videos[0].PublishedAt
	evidence.Videos[0].Title = "mutated input"
	*decision.Videos[0].PublishedAt = originalPublished.Add(time.Hour)
	*decision.EarliestCompleteAt = state.EarliestCompleteAt.Add(time.Hour)
	if decision.Videos[0].Title != "Alpha" || !evidence.Videos[0].PublishedAt.Equal(originalPublished) {
		t.Fatal("decision shares evidence backing storage")
	}
	if !state.EarliestCompleteAt.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("decision shares state backing storage")
	}
}

func TestReducePositiveThenCompleteNegative(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB()}, 0)
	assertVideoPresent(t, got)
	assertMissing(t, got, true)
	assertWithdrawn(t, got, false)
	assertNotifications(t, got)
}

func TestReduceCompleteNegativeThenPositive(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{completeNegativeB(), positiveA()}, 0)
	assertVideoPresent(t, got)
	assertMissing(t, got, true)
	assertWithdrawn(t, got, false)
	assertNotifications(t, got, "vid-a")
}

func TestReducePartialNegativeThenPositive(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{partialNegative(), positiveA()}, 0)
	assertVideoPresent(t, got)
	assertMissing(t, got, false)
	assertNotifications(t, got, "vid-a")
}

func TestReduceLatePositiveClearsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{completeNegativeB(), latePositiveA()}, 0)
	assertVideoPresent(t, got)
	assertMissing(t, got, false)
	assertWithdrawn(t, got, false)
	assertNotifications(t, got, "vid-a")
}

func TestReduceNarrowScopeNegativeDoesNotMarkMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), narrowNegative()}, 0)
	assertVideoPresent(t, got)
	assertMissing(t, got, false)
	assertNotifications(t, got)
}

func TestReduceOneCompleteNegativeOnlyRecordsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB()}, 0)
	assertMissing(t, got, true)
	assertWithdrawn(t, got, false)
	if clockOf(got).ConsecutiveAbsenceSlots != 1 {
		t.Fatalf("consecutive = %d, want 1", clockOf(got).ConsecutiveAbsenceSlots)
	}
}

func TestReduceTwoDistinctNegativesPlusGraceMayTombstone(t *testing.T) {
	t.Parallel()
	grace := time.Hour
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB(), completeNegativeC(grace)}, grace)
	assertMissing(t, got, true)
	assertWithdrawn(t, got, true)
	if clockOf(got).ConsecutiveAbsenceSlots != 2 {
		t.Fatalf("consecutive = %d, want 2", clockOf(got).ConsecutiveAbsenceSlots)
	}
}

func TestReduceReplayPositiveAfterAbsenceKeepsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB(), positiveA()}, 0)
	assertMissing(t, got, true)
	assertWithdrawn(t, got, false)
	assertNotifications(t, got)
}

func TestReduceInterleavedPositiveKeepsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB(), midPositiveA()}, 0)
	assertMissing(t, got, true)
	assertWithdrawn(t, got, false)
	assertNotifications(t, got)
}

func TestReduceLaterPositiveClearsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB(), latePositiveA()}, 0)
	assertMissing(t, got, false)
	assertWithdrawn(t, got, false)
	assertNotifications(t, got)
}

func TestReduceFirstPositiveEmitsNotification(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA()}, 0)
	assertNotifications(t, got, "vid-a")
}

func TestReduceReplayedNegativeDoesNotIncrement(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB(), completeNegativeB()}, 0)
	if clockOf(got).ConsecutiveAbsenceSlots != 1 {
		t.Fatalf("replayed negative consecutive = %d, want 1", clockOf(got).ConsecutiveAbsenceSlots)
	}
	assertWithdrawn(t, got, false)
}

func TestReducePermutationsConverge(t *testing.T) {
	t.Parallel()
	cases := [][]Evidence{
		{positiveA(), completeNegativeB()},
		{completeNegativeB(), positiveA()},
		{partialNegative(), positiveA()},
		{completeNegativeB(), latePositiveA()},
		{positiveA(), narrowNegative()},
		{positiveA(), completeNegativeB(), completeNegativeC(0)},
		{positiveA(), completeNegativeB(), completeNegativeB()},
	}
	for _, evidence := range cases {
		assertAllPermutationsConverge(t, seededState(), evidence, 0)
	}
}

func TestReduceTwoNegativesPlusGracePermutationsConverge(t *testing.T) {
	t.Parallel()
	grace := time.Hour
	assertAllPermutationsConverge(t, seededState(), []Evidence{
		positiveA(), completeNegativeB(), completeNegativeC(grace),
	}, grace)
}
