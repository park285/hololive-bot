package live

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestReduceCopiesInputBackingStorage(t *testing.T) {
	t.Parallel()

	scheduled := time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC)
	originalScheduled := scheduled
	fact := sessionFact("UPCOMING")

	fact.ScheduledAt = &scheduled

	evidence := liveEvidence(1, scheduled, contract.CompletenessComplete, contract.ContinuityContiguous, fact)

	decision, err := Reduce(*emptyState(), evidence, 0, scheduled)
	if err != nil {
		t.Fatal(err)
	}

	*evidence.Sessions[0].ScheduledAt = scheduled.Add(time.Hour)
	if decision.Sessions[0].ScheduledStartTime == nil || !decision.Sessions[0].ScheduledStartTime.Equal(originalScheduled) {
		t.Fatal("decision shares evidence time pointer")
	}

	*decision.Sessions[0].ScheduledStartTime = originalScheduled.Add(2 * time.Hour)
	if !evidence.Sessions[0].ScheduledAt.Equal(originalScheduled.Add(time.Hour)) {
		t.Fatal("evidence shares decision time pointer")
	}
}

func TestReduceUpcomingLiveEndedNormalPath(t *testing.T) {
	t.Parallel()

	got := mustReduceAll(t, emptyState(), []Evidence{upcomingA(), liveA(), endA()}, 0)
	session := sessionOf(got)

	if session.Status != StatusEnded {
		t.Fatalf("status = %s, want ENDED", session.Status)
	}

	if session.EndReason == nil || *session.EndReason != EndReasonExplicitEnd {
		t.Fatalf("end reason = %v", session.EndReason)
	}
}

func TestReduceSparsePositivePreservesLiveMetadata(t *testing.T) {
	t.Parallel()

	metadata := sessionFact("UPCOMING")

	metadata.Title = "Minecraft live"
	metadata.TopicID = "minecraft"
	metadata.ThumbnailURL = "https://i.ytimg.com/vi/vid-a/maxresdefault.jpg"

	first := liveEvidence(
		1,
		time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC),
		contract.CompletenessComplete,
		contract.ContinuityContiguous,
		metadata,
	)
	second := liveEvidence(
		2,
		time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC),
		contract.CompletenessComplete,
		contract.ContinuityContiguous,
		sessionFact("LIVE"),
	)

	session := sessionOf(mustReduceAll(t, emptyState(), []Evidence{first, second}, 0))
	if session.Title != metadata.Title || session.TopicID != metadata.TopicID ||
		session.ThumbnailURL != metadata.ThumbnailURL {
		t.Fatalf("metadata after sparse positive = %#v", session)
	}
}

func TestReducePartialGapTimeoutCannotEnd(t *testing.T) {
	t.Parallel()

	got := mustReduceAll(t, emptyState(), []Evidence{liveA(), partialAbsence()}, time.Hour)
	if sessionOf(got).Status != StatusLive {
		t.Fatal("partial absence must not end")
	}

	got = mustReduceAll(t, emptyState(), []Evidence{liveA(), gapAbsence()}, time.Hour)
	if sessionOf(got).Status != StatusLive {
		t.Fatal("gap absence must not end")
	}

	timeout := liveEvidence(11, time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessUnknown, contract.ContinuityContiguous)

	got = mustReduceAll(t, emptyState(), []Evidence{liveA(), timeout}, time.Hour)

	if sessionOf(got).Status != StatusLive {
		t.Fatal("unknown completeness must not end")
	}
}

func TestReduceCompleteAbsenceBeforeGraceCannotEnd(t *testing.T) {
	t.Parallel()

	grace := 4 * time.Hour
	got := mustReduceAll(t, emptyState(), []Evidence{liveA(), firstAbsence(), secondAbsence(0)}, grace)
	session := sessionOf(got)

	if session.Status == StatusEnded {
		t.Fatal("complete absence before grace must not end")
	}

	if session.Clock.ConsecutiveAbsenceSlots != 2 {
		t.Fatalf("slots = %d, want 2", session.Clock.ConsecutiveAbsenceSlots)
	}
}

func TestReduceOneScopedAbsenceAfterGraceStillCannotEnd(t *testing.T) {
	t.Parallel()

	grace := time.Hour
	second := firstAbsence()

	second.ReceivedAt = second.ReceivedAt.Add(grace)

	got := mustReduceAll(t, emptyState(), []Evidence{liveA(), second}, grace)
	session := sessionOf(got)

	if session.Status == StatusEnded {
		t.Fatal("one scoped absence must not end")
	}

	if session.Clock.ConsecutiveAbsenceSlots != 1 {
		t.Fatalf("slots = %d, want 1", session.Clock.ConsecutiveAbsenceSlots)
	}
}

func TestReduceTwoDistinctScopedAbsenceSlotsAfterGraceCanEnd(t *testing.T) {
	t.Parallel()

	grace := time.Hour
	got := mustReduceAll(t, emptyState(), []Evidence{liveA(), firstAbsence(), secondAbsence(grace)}, grace)
	session := sessionOf(got)

	if session.Status != StatusEnded {
		t.Fatalf("status = %s, want ENDED", session.Status)
	}

	if session.EndReason == nil || *session.EndReason != EndReasonScopedAbsence {
		t.Fatalf("end reason = %v", session.EndReason)
	}
}

func TestReduceExplicitEndAfterFreshnessGraceCanEndWithoutAbsenceCapability(t *testing.T) {
	t.Parallel()

	got := mustReduceAll(t, emptyState(), []Evidence{liveA(), endA()}, 0)

	if sessionOf(got).Status != StatusEnded {
		t.Fatal("explicit end must not require absence capability")
	}
}

func TestReduceExplicitCancellationEndsNeverLiveUpcoming(t *testing.T) {
	t.Parallel()

	got := mustReduceAll(t, emptyState(), []Evidence{upcomingA(), cancelA()}, 0)
	session := sessionOf(got)

	if session.Status != StatusEnded {
		t.Fatalf("status = %s, want ENDED", session.Status)
	}

	if session.EndReason == nil || *session.EndReason != EndReasonCancelledBeforeLive {
		t.Fatalf("end reason = %v", session.EndReason)
	}
}

func TestReduceScopedAbsenceCannotEndNeverLiveUpcoming(t *testing.T) {
	t.Parallel()

	got := mustReduceAll(t, emptyState(), []Evidence{upcomingA(), firstAbsence(), secondAbsence(0)}, 0)
	session := sessionOf(got)

	if session.Status != StatusUpcoming {
		t.Fatal("scoped absence must not end never-live UPCOMING")
	}

	if session.Clock.ConsecutiveAbsenceSlots != 0 {
		t.Fatalf("never-live UPCOMING slots = %d, want 0", session.Clock.ConsecutiveAbsenceSlots)
	}
}

func TestReduceLiveOnlySnapshotDoesNotAbsentUpcoming(t *testing.T) {
	t.Parallel()

	liveOnly := firstAbsence()

	liveOnly.Coverage.Filters.Statuses = []string{"LIVE"}

	got := mustReduceAll(t, emptyState(), []Evidence{upcomingA(), liveOnly}, 0)

	if sessionOf(got).Clock.ConsecutiveAbsenceSlots != 0 {
		t.Fatal("LIVE-only complete snapshot must not count as UPCOMING absence")
	}
}

func TestReduceUpcomingPeriodSlotsDoNotSatisfyLiveEnd(t *testing.T) {
	t.Parallel()

	got := mustReduceAll(t, emptyState(), []Evidence{upcomingA(), firstAbsence(), secondAbsence(0), liveA()}, 0)
	session := sessionOf(got)

	if session.Status != StatusLive {
		t.Fatalf("status = %s, want LIVE", session.Status)
	}

	if session.Clock.ConsecutiveAbsenceSlots != 0 {
		t.Fatalf("pre-LIVE slots leaked into LIVE end count: %d", session.Clock.ConsecutiveAbsenceSlots)
	}
}

func TestReduceLatePositivePreventsEnd(t *testing.T) {
	t.Parallel()

	grace := 4 * time.Hour
	got := mustReduceAll(t, emptyState(), []Evidence{liveA(), endA(), lateLiveA()}, grace)
	session := sessionOf(got)

	if session.Status == StatusEnded {
		t.Fatal("newer positive must prevent end")
	}

	if session.Clock.EndCandidateKind != nil {
		t.Fatal("newer positive must clear end candidate")
	}

	got = mustReduceAll(t, emptyState(), []Evidence{liveA(), endA(), sameTimeLiveA()}, grace)
	session = sessionOf(got)

	if session.Status == StatusEnded {
		t.Fatal("same-time positive must prevent end")
	}

	if session.Clock.EndCandidateKind != nil {
		t.Fatal("same-time positive must clear end candidate")
	}
}

func TestReduceAlreadyEndedNeverReturnsLive(t *testing.T) {
	t.Parallel()

	got := mustReduceAll(t, emptyState(), []Evidence{liveA(), endA(), lateLiveA()}, 0)
	if sessionOf(got).Status != StatusEnded {
		t.Fatal("already ENDED session must stay ENDED")
	}
}

func TestReduceSameLiveEvidencePermutationsConverge(t *testing.T) {
	t.Parallel()
	assertAllPermutationsConverge(t, emptyState(), []Evidence{upcomingA(), liveA()}, 0)
	assertAllPermutationsConverge(t, emptyState(), []Evidence{liveA(), endA()}, 0)
	assertAllPermutationsConverge(t, emptyState(), []Evidence{upcomingA(), cancelA()}, 0)
	assertAllPermutationsConverge(t, emptyState(), []Evidence{liveA(), firstAbsence()}, 0)
	assertAllPermutationsConverge(t, emptyState(), []Evidence{liveA(), lateLiveA()}, 0)
}

func TestReduceTwoAbsencesPlusGracePermutationsConverge(t *testing.T) {
	t.Parallel()

	grace := time.Hour
	assertAllPermutationsConverge(t, emptyState(), []Evidence{liveA(), firstAbsence(), secondAbsence(grace)}, grace)
}

func TestReduceCollectorClockDoesNotShortenGrace(t *testing.T) {
	t.Parallel()

	grace := 4 * time.Hour
	end := endA()

	end.ReceivedAt = liveA().ReceivedAt.Add(time.Minute)

	got := mustReduceAll(t, emptyState(), []Evidence{liveA(), end}, grace)

	if sessionOf(got).Status == StatusEnded {
		t.Fatal("collector-facing observation time must not shorten grace")
	}
}

func TestCanEndMatchesContract(t *testing.T) {
	t.Parallel()

	liveAt := time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC)
	seenAt := liveAt
	clock := LiveEvidenceClock{LastLivePositiveAt: &liveAt, LastLivePositiveSeenAt: &seenAt, ConsecutiveAbsenceSlots: 2}
	endAt := liveAt.Add(time.Hour)

	if !CanEnd(&clock, &EndEvidence{
		Kind: EndEvidenceExplicitEnd, EffectiveAt: endAt, Valid: true, EntityMatchesSession: true,
	}, seenAt.Add(time.Minute), 0) {
		t.Fatal("explicit end after live should end")
	}

	if CanEnd(&clock, &EndEvidence{
		Kind: EndEvidenceExplicitEnd, EffectiveAt: liveAt, Valid: true, EntityMatchesSession: true,
	}, seenAt.Add(time.Minute), 0) {
		t.Fatal("equal-time explicit end must not end")
	}
}
