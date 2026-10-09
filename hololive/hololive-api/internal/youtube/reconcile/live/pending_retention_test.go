package live

import (
	"reflect"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 읽기 모델은 PendingEnds 존재만으로 현재 미해결 종료를 판정하면 안 된다. 비ENDED 세션에서
// positive에 밀린 종료 증거는 보존하고, 이미 ENDED가 된 세션의 반복 종료는 다시 보관하지 않는다.
func TestReduceRetainsSupersededEndEvidence(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		evidence []Evidence
		status   domain.LiveStatus
		pending  int
	}{
		{name: "repeated terminal end", evidence: []Evidence{liveA(), endA(), endA()}, status: domain.LiveStatusEnded, pending: 0},
		{name: "older end after newer live", evidence: []Evidence{lateLiveA(), endA()}, status: domain.LiveStatusLive, pending: 1},
		{name: "same-time end after live", evidence: []Evidence{sameTimeLiveA(), endA()}, status: domain.LiveStatusLive, pending: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := mustReduceAll(t, emptyState(), tc.evidence, 0)
			session := sessionOf(got)

			if session.Status != tc.status || session.Clock.EndCandidateKind != nil || len(got.PendingEnds) != tc.pending {
				t.Fatalf("status=%s candidate=%v pending=%d want %d", session.Status, session.Clock.EndCandidateKind, len(got.PendingEnds), tc.pending)
			}
		})
	}
}

// storedEndedSession은 DB에서 읽은 ENDED 세션이다. ENDED 세션은 무시한 부재 이력을 적재하지 않는다.
func storedEndedSession() SessionState {
	started := liveA().EffectiveAt
	ended := endA().EffectiveAt
	reason := EndReasonExplicitEnd

	return SessionState{
		VideoID: testVideoID, ChannelID: channelCoverage().RequestedChannelIDs[0], Status: domain.LiveStatusEnded, LifecycleOrigin: OriginObserved,
		StartedAt: &started, EndedAt: &ended, LiveFirstSeenAt: &started, LastSeenAt: ended, StatusObservedAt: &ended,
		EndReason: &reason,
		Clock: LiveEvidenceClock{
			LastLivePositiveAt: &started, LastLivePositiveSeenAt: &started, LastEndEvidenceAt: &ended, EndedAt: &ended,
		},
		Present: true, HeadPresent: true,
	}
}

func pendingByVideo(items []PendingEnd) map[string]PendingEnd {
	pending := make(map[string]PendingEnd, len(items))
	for i := range items {
		pending[items[i].VideoID] = items[i]
	}

	return pending
}

// 저장된 ENDED 세션은 되살아나지 않으므로 반복 종료·취소를 pending으로 다시 쓰지 않는다.
// 이미 있는 pending(D2 진단)은 지우지도 새 관측으로 바꾸지도 않는다.
func TestReduceSkipsRepeatedEndForStoredEndedSession(t *testing.T) {
	t.Parallel()

	repeatedAt := endA().EffectiveAt.Add(2 * time.Hour)
	ended, canceled := endA().Sessions[0].Status, cancelA().Sessions[0].Status
	stored := map[string]PendingEnd{testVideoID: {
		Kind: EndEvidenceExplicitEnd, VideoID: testVideoID, ChannelID: channelCoverage().RequestedChannelIDs[0],
		ObservationID: endA().ObservationID, EffectiveAt: endA().EffectiveAt, ReceivedAt: endA().ReceivedAt,
		ScheduledFor: endA().ScheduledFor, NegativeEligible: true, ScopeCovers: true,
	}}

	for _, tc := range []struct {
		name    string
		status  string
		pending map[string]PendingEnd
	}{
		{name: "ended without pending", status: ended, pending: map[string]PendingEnd{}},
		{name: "ended with older pending", status: ended, pending: stored},
		{name: "canceled without pending", status: canceled, pending: map[string]PendingEnd{}},
		{name: "canceled with older pending", status: canceled, pending: stored},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			state := State{Sessions: map[string]SessionState{testVideoID: storedEndedSession()}, PendingEnds: tc.pending}
			evidence := liveEvidence(20, repeatedAt, contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact(tc.status))

			decision, err := Reduce(state, evidence, 0, evidence.ReceivedAt)
			if err != nil {
				t.Fatal(err)
			}

			if len(decision.Sessions) != 0 || len(decision.Applications) != 0 {
				t.Fatalf("ENDED session changed: sessions=%#v applications=%#v", decision.Sessions, decision.Applications)
			}

			if got := pendingByVideo(decision.PendingEnds); !reflect.DeepEqual(got, tc.pending) {
				t.Fatalf("pending = %#v, want %#v", got, tc.pending)
			}
		})
	}
}

// 세션 행이 없는 영상의 종료는 늦게 도착할 positive를 위해 최신 관측으로 계속 보관한다.
func TestReduceKeepsNewestEndForVideoWithoutSession(t *testing.T) {
	t.Parallel()

	later := liveEvidence(21, endA().EffectiveAt.Add(2*time.Hour), contract.CompletenessComplete, contract.ContinuityContiguous,
		sessionFact(string(domain.LiveStatusEnded)))
	state := *emptyState()

	for _, evidence := range []Evidence{endA(), later} {
		decision, err := Reduce(state, evidence, 0, evidence.ReceivedAt)
		if err != nil {
			t.Fatal(err)
		}

		state = stateFromDecision(&state, &decision)
	}

	if pending, ok := state.PendingEnds[testVideoID]; !ok || pending.ObservationID != later.ObservationID {
		t.Fatalf("pending = %#v, want observation %d", state.PendingEnds, later.ObservationID)
	}

	// 두 종료 사이에 시작한 LIVE가 늦게 도착하면 최신 종료로 끝난다. 첫 종료만 남았다면 LIVE로 남는다.
	lateLive := liveEvidence(22, endA().EffectiveAt.Add(time.Hour), contract.CompletenessPartial, contract.ContinuityContiguous,
		sessionFact(testLiveStatus))

	decision, err := Reduce(state, lateLive, 0, lateLive.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}

	session := sessionOf(&decision)
	if session.Status != domain.LiveStatusEnded || session.EndedAt == nil || !session.EndedAt.Equal(later.EffectiveAt) ||
		len(decision.PendingEnds) != 0 {
		t.Fatalf("late live was not ended by newest pending: session=%#v pending=%#v", session, decision.PendingEnds)
	}
}

// session 행 없이 head만 남은 ENDED 영상은 저장된 ENDED 세션이 아니므로 종료를 지금처럼 기록한다.
func TestReduceRecordsEndForHeadOnlyEndedVideo(t *testing.T) {
	t.Parallel()

	headOnly := storedEndedSession()

	headOnly.Present = false

	state := State{Sessions: map[string]SessionState{testVideoID: headOnly}, PendingEnds: map[string]PendingEnd{}}
	evidence := liveEvidence(23, endA().EffectiveAt.Add(time.Hour), contract.CompletenessComplete, contract.ContinuityContiguous,
		sessionFact(string(domain.LiveStatusEnded)))

	decision, err := Reduce(state, evidence, 0, evidence.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}

	if len(decision.Sessions) != 0 || len(decision.Applications) != 0 || len(decision.PendingEnds) != 1 ||
		decision.PendingEnds[0].ObservationID != evidence.ObservationID {
		t.Fatalf("head-only ENDED decision changed: %#v", decision)
	}
}
