package content

import (
	"testing"
	"time"
)

func TestReducePositiveThenCompleteNegative(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB()}, 0)
	assertVideoPresent(t, got, "vid-a")
	assertMissing(t, got, "vid-a", true)
	assertWithdrawn(t, got, "vid-a", false)
	assertNotifications(t, got)
}

func TestReduceCompleteNegativeThenPositive(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{completeNegativeB(), positiveA()}, 0)
	assertVideoPresent(t, got, "vid-a")
	assertMissing(t, got, "vid-a", true)
	assertWithdrawn(t, got, "vid-a", false)
	assertNotifications(t, got, "vid-a")
}

func TestReducePartialNegativeThenPositive(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{partialNegative(), positiveA()}, 0)
	assertVideoPresent(t, got, "vid-a")
	assertMissing(t, got, "vid-a", false)
	assertNotifications(t, got, "vid-a")
}

func TestReduceLatePositiveClearsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{completeNegativeB(), latePositiveA()}, 0)
	assertVideoPresent(t, got, "vid-a")
	assertMissing(t, got, "vid-a", false)
	assertWithdrawn(t, got, "vid-a", false)
	assertNotifications(t, got, "vid-a")
}

func TestReduceNarrowScopeNegativeDoesNotMarkMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), narrowNegative()}, 0)
	assertVideoPresent(t, got, "vid-a")
	assertMissing(t, got, "vid-a", false)
	assertNotifications(t, got)
}

func TestReduceOneCompleteNegativeOnlyRecordsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB()}, 0)
	assertMissing(t, got, "vid-a", true)
	assertWithdrawn(t, got, "vid-a", false)
	if clockOf(got, "vid-a").ConsecutiveAbsenceSlots != 1 {
		t.Fatalf("consecutive = %d, want 1", clockOf(got, "vid-a").ConsecutiveAbsenceSlots)
	}
}

func TestReduceTwoDistinctNegativesPlusGraceMayTombstone(t *testing.T) {
	t.Parallel()
	grace := time.Hour
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB(), completeNegativeC(grace)}, grace)
	assertMissing(t, got, "vid-a", true)
	assertWithdrawn(t, got, "vid-a", true)
	if clockOf(got, "vid-a").ConsecutiveAbsenceSlots != 2 {
		t.Fatalf("consecutive = %d, want 2", clockOf(got, "vid-a").ConsecutiveAbsenceSlots)
	}
}

func TestReduceReplayPositiveAfterAbsenceKeepsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB(), positiveA()}, 0)
	assertMissing(t, got, "vid-a", true)
	assertWithdrawn(t, got, "vid-a", false)
	assertNotifications(t, got)
}

func TestReduceInterleavedPositiveKeepsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB(), midPositiveA()}, 0)
	assertMissing(t, got, "vid-a", true)
	assertWithdrawn(t, got, "vid-a", false)
	assertNotifications(t, got)
}

func TestReduceLaterPositiveClearsMissing(t *testing.T) {
	t.Parallel()
	got := mustReduceAll(t, seededState(), []Evidence{positiveA(), completeNegativeB(), latePositiveA()}, 0)
	assertMissing(t, got, "vid-a", false)
	assertWithdrawn(t, got, "vid-a", false)
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
	if clockOf(got, "vid-a").ConsecutiveAbsenceSlots != 1 {
		t.Fatalf("replayed negative consecutive = %d, want 1", clockOf(got, "vid-a").ConsecutiveAbsenceSlots)
	}
	assertWithdrawn(t, got, "vid-a", false)
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
