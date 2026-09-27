package live

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func videoCheckEvidence(id int64, at time.Time, facts ...SessionFact) Evidence {
	evidence := liveEvidence(id, at, contract.CompletenessPartial, contract.ContinuityNotApplicable, facts...)

	evidence.Kind = contract.KindVideoLiveCheck
	evidence.Coverage = contract.GlobalChannelCoverageV1{}

	return evidence
}

// 영상 확인의 종료는 관측 슬롯이 아니라 upstream 종료 시각으로 기록되고 absence slot을 만들지 않는다.
func TestReduceVideoLiveCheckEndUsesUpstreamEndedAt(t *testing.T) {
	endedAt := time.Date(2026, time.August, 14, 2, 30, 0, 0, time.UTC)
	checkAt := time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC)
	fact := sessionFact("ENDED")

	fact.EndedAt = &endedAt

	state := stateFromDecision(emptyState(), mustReduce(t, emptyState(), liveA()))

	decision := mustReduce(t, &state, videoCheckEvidence(20, checkAt, fact))
	session := sessionOf(decision)

	if session.Status != StatusEnded || session.EndedAt == nil || !session.EndedAt.Equal(endedAt) {
		t.Fatalf("status=%s ended_at=%v, want ENDED at %s", session.Status, session.EndedAt, endedAt)
	}

	if session.EndReason == nil || *session.EndReason != EndReasonExplicitEnd {
		t.Fatalf("end reason = %v, want %s", session.EndReason, EndReasonExplicitEnd)
	}

	if decision.AbsenceSlot != nil {
		t.Fatalf("video live check created absence slot %+v", decision.AbsenceSlot)
	}
}

// 사실 없는 영상 확인은 부재가 아니므로 LIVE를 끝내거나 absence 수를 세지 않는다.
func TestReduceVideoLiveCheckWithoutFactsIsNotAbsence(t *testing.T) {
	state := stateFromDecision(emptyState(), mustReduce(t, emptyState(), liveA()))

	for i, at := range []time.Time{
		time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 14, 4, 0, 0, 0, time.UTC),
	} {
		decision := mustReduce(t, &state, videoCheckEvidence(int64(30+i), at))

		state = stateFromDecision(&state, decision)

		if decision.AbsenceSlot != nil {
			t.Fatalf("video live check %d created absence slot", i)
		}
	}

	session := state.Sessions[testVideoID]
	if session.Status != StatusLive || session.Clock.ConsecutiveAbsenceSlots != 0 || len(state.PendingEnds) != 0 {
		t.Fatalf("status=%s slots=%d pending=%d", session.Status, session.Clock.ConsecutiveAbsenceSlots, len(state.PendingEnds))
	}
}

func TestReduceRejectsKindsWithoutLifecycleFacts(t *testing.T) {
	for _, kind := range []contract.ObservationKind{contract.KindChannelLiveCheck, contract.KindViewerSample} {
		evidence := liveA()

		evidence.Kind = kind

		if _, err := Reduce(*emptyState(), evidence, 0, evidence.ReceivedAt); err == nil {
			t.Fatalf("reducer admitted %s", kind)
		}
	}
}

func mustReduce(t *testing.T, state *State, evidence Evidence) *Decision {
	t.Helper()

	decision, err := Reduce(*state, evidence, 0, evidence.ReceivedAt)
	if err != nil {
		t.Fatalf("reduce: %v", err)
	}

	return &decision
}
