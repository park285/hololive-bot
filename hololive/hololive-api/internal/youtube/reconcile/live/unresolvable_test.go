package live

import (
	"testing"
	"time"

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

// trackedUnresolvableState는 시작을 관측한 LIVE가 첫 identity_missing 확인으로 추적된 상태다.
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

// 추적된 세션은 검증되지 않은 사실이나 마지막 positive의 grace 안에서는 LIVE로 남고 첫 관측 시각을 유지한다.
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

// consumer가 검증한 사실은 마지막 positive의 grace가 지나면 UNRESOLVABLE_VIDEO로 끝낸다. 종료 시각은 첫
// identity_missing 시각이고 시작 시각·positive clock은 유지하며 pending·absence slot을 만들지 않는다.
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

// positive는 영상이 아직 해석됨을 뜻하므로 추적을 지우고, 시작을 관측하지 못한 UPCOMING은 추적하지 않는다.
func TestReduceUnresolvableVideoClearedByPositiveAndIgnoredWithoutLiveStart(t *testing.T) {
	state := trackedUnresolvableState(t)

	cleared, _ := reduceAt(t, &state, lateLiveA(), 0)
	if session := sessionOf(&cleared); session.Clock.UnresolvableSince != nil || session.Status != domain.LiveStatusLive {
		t.Fatalf("live positive kept unresolvable tracking: %+v", session)
	}

	upcoming := stateFromDecision(emptyState(), mustReduce(t, emptyState(), upcomingA()))

	ignored, upcoming := reduceAt(t, &upcoming, videoCheckEvidence(33, unresolvableFirstAt, sessionFact(StatusUnresolvable)), 0)
	if session := upcoming.Sessions[testVideoID]; !hasDecision(&ignored, "UNRESOLVABLE_IGNORED") || session.Clock.UnresolvableSince != nil ||
		session.Status != domain.LiveStatusUpcoming {
		t.Fatalf("upcoming identity_missing = %+v decisions=%+v, want ignored", session, ignored.Applications)
	}
}
