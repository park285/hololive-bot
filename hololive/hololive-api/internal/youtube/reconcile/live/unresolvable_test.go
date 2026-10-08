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

// positive는 영상이 아직 해석됨을 뜻하므로 추적을 지운다.
func TestReduceUnresolvableVideoClearedByPositive(t *testing.T) {
	state := trackedUnresolvableState(t)

	cleared, _ := reduceAt(t, &state, lateLiveA(), 0)
	if session := sessionOf(&cleared); session.Clock.UnresolvableSince != nil || session.Status != domain.LiveStatusLive {
		t.Fatalf("live positive kept unresolvable tracking: %+v", session)
	}
}

// 시작을 관측하지 못한 UPCOMING은 첫 관측 시각을 추적만 하고, 검증된 사실이 grace 뒤에 와도 끝내지 않는다.
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

// 관측 시각 이후 positive가 있으면 그 identity_missing은 더 오래된 사실이므로 추적을 시작하지 않는다.
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

// 추적 시작보다 이른 관측이 늦게 도착한 positive·시작 미확정 LIVE는 추적을 지우지 않는다.
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

// 이전 바이너리 기간에 positive가 지우지 못한 잔여값(positive보다 이른 추적)은 종료 근거가 아니다. 검증된 사실이
// grace 뒤에 와도 끝내지 않고 이번 관측부터 다시 추적한다.
func TestReduceUnresolvableVideoRestartsStaleTracking(t *testing.T) {
	state := stateFromDecision(emptyState(), mustReduce(t, emptyState(), liveA()))
	session := state.Sessions[testVideoID]
	stale := time.Date(2026, time.August, 14, 1, 30, 0, 0, time.UTC)

	session.Clock.UnresolvableSince = &stale
	state.Sessions[testVideoID] = session

	verified := sessionFact(StatusUnresolvable)

	verified.VerifiedTerminal = true

	restarted, next := reduceAt(t, &state, videoCheckEvidence(43, unresolvableLaterAt, verified), time.Hour)
	if got := next.Sessions[testVideoID]; !hasDecision(&restarted, "UNRESOLVABLE_TRACKED") || got.Status != domain.LiveStatusLive ||
		got.Clock.UnresolvableSince == nil || !got.Clock.UnresolvableSince.Equal(unresolvableLaterAt) {
		t.Fatalf("stale tracking = %+v decisions=%+v, want tracking restarted at %s", got, restarted.Applications, unresolvableLaterAt)
	}
}

// head 없는 metadata_only UPCOMING은 head를 만들 근거가 없으므로 추적을 기록하지 않는다.
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
