package live

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func hasDecision(decision *Decision, want string) bool {
	for _, application := range decision.Applications {
		if application.EntityKey == testVideoID && application.Decision == want {
			return true
		}
	}

	return false
}

func reduceAt(t *testing.T, state *State, evidence Evidence, grace time.Duration) (Decision, State) {
	t.Helper()

	decision, err := Reduce(*state, evidence, grace, evidence.ReceivedAt)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}

	next := stateFromDecision(state, &decision)

	return decision, next
}

var (
	unresolvableFirstAt = time.Date(2026, time.August, 14, 2, 10, 0, 0, time.UTC)
	unresolvableLaterAt = time.Date(2026, time.August, 14, 3, 10, 0, 0, time.UTC)
)

func trackedUnresolvableState(t *testing.T) State {
	t.Helper()

	state := stateFromDecision(emptyState(), mustReduce(t, emptyState(), liveA()))

	tracked, state := reduceAt(t, &state, videoCheckEvidence(30, unresolvableFirstAt, sessionFact(StatusUnresolvable)), time.Hour)
	if session := sessionOf(&tracked); !hasDecision(&tracked, "UNRESOLVABLE_TRACKED") || session.Status != domain.LiveStatusLive ||
		session.Clock.UnresolvableSince == nil || !session.Clock.UnresolvableSince.Equal(unresolvableFirstAt) {
		t.Fatalf("first identity_missing = %+v decisions=%+v, want tracked LIVE since %s", session, tracked.Applications, unresolvableFirstAt)
	}

	return state
}

func TestReduceUnresolvableVideoRetainsUntilVerifiedAfterGrace(t *testing.T) {
	state := trackedUnresolvableState(t)
	verified := sessionFact(StatusUnresolvable)

	verified.VerifiedTerminal = true

	for _, tc := range []struct {
		name string
		fact SessionFact
		at   time.Time
	}{
		{name: "verified inside positive grace", fact: verified, at: time.Date(2026, time.August, 14, 2, 20, 0, 0, time.UTC)},
		{name: "unverified after grace", fact: sessionFact(StatusUnresolvable), at: unresolvableLaterAt},
	} {
		retained, next := reduceAt(t, &state, videoCheckEvidence(31, tc.at, tc.fact), time.Hour)
		if session := next.Sessions[testVideoID]; !hasDecision(&retained, "UNRESOLVABLE_RETAINED") || session.Status != domain.LiveStatusLive ||
			!session.Clock.UnresolvableSince.Equal(unresolvableFirstAt) {
			t.Fatalf("%s: session=%+v decisions=%+v, want retained LIVE", tc.name, session, retained.Applications)
		}
	}
}

func TestReduceUnresolvableVideoEndsAfterGrace(t *testing.T) {
	state := trackedUnresolvableState(t)
	verified := sessionFact(StatusUnresolvable)

	verified.VerifiedTerminal = true

	ended, _ := reduceAt(t, &state, videoCheckEvidence(32, unresolvableLaterAt, verified), time.Hour)
	session := sessionOf(&ended)

	if !hasDecision(&ended, "ENDED") || session.Status != domain.LiveStatusEnded || session.EndedAt == nil || !session.EndedAt.Equal(unresolvableFirstAt) ||
		session.EndReason == nil || *session.EndReason != EndReasonUnresolvableVideo || session.StartedAt == nil ||
		session.Clock.LastLivePositiveAt == nil || session.Clock.NextEndCheckAt != nil {
		t.Fatalf("verified identity_missing after grace = %+v decisions=%+v, want UNRESOLVABLE_VIDEO ended at %s", session, ended.Applications, unresolvableFirstAt)
	}

	if len(ended.PendingEnds) != 0 || ended.AbsenceSlot != nil {
		t.Fatalf("unresolvable end left pending %+v or absence slot %+v", ended.PendingEnds, ended.AbsenceSlot)
	}
}

func TestReduceUnresolvableVideoClearedByPositive(t *testing.T) {
	state := trackedUnresolvableState(t)

	cleared, _ := reduceAt(t, &state, lateLiveA(), 0)
	if session := sessionOf(&cleared); session.Clock.UnresolvableSince != nil || session.Status != domain.LiveStatusLive {
		t.Fatalf("live positive kept unresolvable tracking: %+v", session)
	}
}

func TestReduceUnresolvableVideoTracksUpcomingWithoutEnding(t *testing.T) {
	state := stateFromDecision(emptyState(), mustReduce(t, emptyState(), upcomingA()))
	verified := sessionFact(StatusUnresolvable)

	verified.VerifiedTerminal = true

	tracked, state := reduceAt(t, &state, videoCheckEvidence(33, unresolvableFirstAt, sessionFact(StatusUnresolvable)), 0)
	if session := sessionOf(&tracked); !hasDecision(&tracked, "UNRESOLVABLE_TRACKED") || session.Clock.UnresolvableSince == nil ||
		!session.Clock.UnresolvableSince.Equal(unresolvableFirstAt) || session.Status != domain.LiveStatusUpcoming {
		t.Fatalf("upcoming identity_missing = %+v decisions=%+v, want tracked UPCOMING", session, tracked.Applications)
	}

	retained, state := reduceAt(t, &state, videoCheckEvidence(34, unresolvableLaterAt, verified), 0)
	if session := state.Sessions[testVideoID]; !hasDecision(&retained, "UNRESOLVABLE_RETAINED") || session.Status != domain.LiveStatusUpcoming ||
		session.EndedAt != nil || !session.Clock.UnresolvableSince.Equal(unresolvableFirstAt) {
		t.Fatalf("verified fact on upcoming = %+v decisions=%+v, want retained UPCOMING", session, retained.Applications)
	}

	laterUpcoming := liveEvidence(35, time.Date(2026, time.August, 14, 3, 20, 0, 0, time.UTC),
		contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("UPCOMING"))

	cleared, _ := reduceAt(t, &state, laterUpcoming, 0)
	if session := sessionOf(&cleared); session.Clock.UnresolvableSince != nil || session.Status != domain.LiveStatusUpcoming {
		t.Fatalf("upcoming positive kept unresolvable tracking: %+v", session)
	}
}

func TestReduceUnresolvableVideoRetainsNewerPositive(t *testing.T) {
	state := stateFromDecision(emptyState(), mustReduce(t, emptyState(), liveA()))

	for _, at := range []time.Time{
		time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 14, 1, 59, 0, 0, time.UTC),
	} {
		retained, next := reduceAt(t, &state, videoCheckEvidence(40, at, sessionFact(StatusUnresolvable)), 0)
		if session := next.Sessions[testVideoID]; !hasDecision(&retained, "NEWER_POSITIVE_RETAINED") ||
			session.Clock.UnresolvableSince != nil || session.Status != domain.LiveStatusLive {
			t.Fatalf("identity_missing at %s = %+v decisions=%+v, want NEWER_POSITIVE_RETAINED without tracking", at, session, retained.Applications)
		}
	}
}

func TestReduceUnresolvableVideoKeepsTrackingForOlderPositive(t *testing.T) {
	state := trackedUnresolvableState(t)
	olderLive := liveEvidence(41, time.Date(2026, time.August, 14, 2, 5, 0, 0, time.UTC),
		contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact(testLiveStatus))
	unconfirmed := sessionFact(testLiveStatus)

	unconfirmed.LiveStartConfirmed = false

	olderUnconfirmed := liveEvidence(42, time.Date(2026, time.August, 14, 1, 50, 0, 0, time.UTC),
		contract.CompletenessComplete, contract.ContinuityContiguous, unconfirmed)

	for _, evidence := range []Evidence{olderLive, olderUnconfirmed} {
		_, next := reduceAt(t, &state, evidence, 0)
		if session := next.Sessions[testVideoID]; session.Clock.UnresolvableSince == nil ||
			!session.Clock.UnresolvableSince.Equal(unresolvableFirstAt) || session.Status != domain.LiveStatusLive {
			t.Fatalf("older evidence at %s cleared tracking: %+v", evidence.EffectiveAt, session)
		}
	}
}

func TestReduceUnresolvableVideoIgnoresHeadlessMetadataUpcoming(t *testing.T) {
	state := emptyState()

	state.Sessions[testVideoID] = SessionState{
		VideoID: testVideoID, ChannelID: "UC_TEST", Status: domain.LiveStatusUpcoming,
		LifecycleOrigin: OriginMetadataOnly, Present: true,
	}

	ignored, next := reduceAt(t, state, videoCheckEvidence(44, unresolvableFirstAt, sessionFact(StatusUnresolvable)), 0)
	if got := next.Sessions[testVideoID]; !hasDecision(&ignored, "UNRESOLVABLE_IGNORED") || got.Clock.UnresolvableSince != nil || len(ignored.Sessions) != 0 {
		t.Fatalf("headless metadata upcoming = %+v decisions=%+v sessions=%d, want ignored without persistence", got, ignored.Applications, len(ignored.Sessions))
	}
}
