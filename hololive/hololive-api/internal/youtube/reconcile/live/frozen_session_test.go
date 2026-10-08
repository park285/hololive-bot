package live

import (
	"reflect"
	"slices"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	frozenCompanionID = "vid-b"
	frozenDueID       = "vid-due"
)

type frozenScenario struct {
	name   string
	slots  []AbsenceSlot
	reduce func(State) (Decision, error)
	dirty  []string
}

// frozenBaseState는 종료 후보 없는 저장된 ENDED 세션 하나와 대조군 두 개를 담는다. 대조군은 부재를
// 셀 수 있는 LIVE 세션과, session은 ENDED인데 due 종료 후보가 남은 세션이다.
func frozenBaseState(frozen SessionState, frozenPending *PendingEnd, slots []AbsenceSlot) State {
	companion := scopeObserved(frozenCompanionID, domain.LiveStatusLive)

	companion.StartedAt = new(liveA().EffectiveAt)
	companion.Clock.LastLivePositiveAt = new(liveA().EffectiveAt)
	companion.Clock.LastLivePositiveSeenAt = new(liveA().EffectiveAt)

	due := scopeEnded(frozenDueID, 0)
	kind := EndEvidenceExplicitEnd

	due.IgnoredAbsences = IgnoredAbsenceHistory{}
	due.Clock.EndCandidateKind = &kind
	due.Clock.EndCandidateObservationID = new(int64(901))
	due.Clock.NextEndCheckAt = new(endA().EffectiveAt.Add(30 * time.Minute))

	state := State{
		Sessions: map[string]SessionState{
			testVideoID: frozen, frozenCompanionID: companion, frozenDueID: due,
		},
		PendingEnds: map[string]PendingEnd{
			frozenDueID: scopePending(frozenDueID, 901, endA().EffectiveAt),
		},
		AbsenceSlots: slices.Clone(slots),
	}

	if frozenPending != nil {
		state.PendingEnds[testVideoID] = *frozenPending
	}

	return state
}

func frozenScenarios() []frozenScenario {
	at := endA().EffectiveAt.Add(2 * time.Hour)
	endedAt := at.Add(-30 * time.Minute)
	partial := func(id int64, fact SessionFact) func(State) (Decision, error) {
		evidence := liveEvidence(id, at, contract.CompletenessPartial, contract.ContinuityContiguous, fact)

		return func(state State) (Decision, error) { return Reduce(state, evidence, time.Hour, at) }
	}
	fact := func(status string, edit func(*SessionFact)) SessionFact {
		item := sessionFact(status)
		edit(&item)

		return item
	}
	keep := func(*SessionFact) {}
	ended := func(item *SessionFact) { item.EndedAt = &endedAt }
	verified := func(item *SessionFact) {
		item.EndedAt = &endedAt
		item.VerifiedTerminal = true
	}
	unconfirmed := func(item *SessionFact) { item.LiveStartConfirmed = false }
	absence := liveEvidence(40, at, contract.CompletenessComplete, contract.ContinuityContiguous)
	videoEnd := videoCheckEvidence(41, at, fact(string(domain.LiveStatusEnded), verified))
	stored := AbsenceSlot{
		ObservationID: 39, ScheduledFor: at.Add(-time.Hour), EffectiveAt: at.Add(-time.Hour), ReceivedAt: at.Add(-time.Hour),
		Coverage: channelCoverage(),
	}

	return []frozenScenario{
		{name: "upcoming", reduce: partial(30, fact("UPCOMING", keep)), dirty: []string{frozenDueID}},
		{name: "live", reduce: partial(31, fact(testLiveStatus, keep)), dirty: []string{frozenDueID}},
		{name: "live start unconfirmed", reduce: partial(32, fact(testLiveStatus, unconfirmed)), dirty: []string{frozenDueID}},
		{name: "ended", reduce: partial(33, fact(string(domain.LiveStatusEnded), ended)), dirty: []string{frozenDueID}},
		{name: "explicit cancel", reduce: partial(34, fact(cancelA().Sessions[0].Status, keep)), dirty: []string{frozenDueID}},
		{
			name:   "verified video end",
			reduce: func(state State) (Decision, error) { return Reduce(state, videoEnd, time.Hour, at) },
			dirty:  []string{frozenDueID},
		},
		{
			name:   "scoped absence",
			reduce: func(state State) (Decision, error) { return Reduce(state, absence, time.Hour, at) },
			dirty:  []string{frozenCompanionID, frozenDueID},
		},
		{
			name:   "stored absence replay",
			slots:  []AbsenceSlot{stored},
			reduce: partial(35, fact(testLiveStatus, keep)),
			dirty:  []string{frozenCompanionID, frozenDueID},
		},
		{
			name:   "due finalize",
			reduce: func(state State) (Decision, error) { return FinalizeDue(state, at, time.Hour), nil },
			dirty:  []string{frozenDueID},
		},
	}
}

func frozenVariants() map[string]SessionState {
	headless := storedEndedSession()

	headless.HeadPresent = false
	headless.Clock = LiveEvidenceClock{}
	headless.EndReason = nil
	headless.IgnoredAbsences = LoadedIgnoredAbsences(nil)

	return map[string]SessionState{"with head": storedEndedSession(), "without head": headless}
}

func dirtyIDs(decision *Decision) []string {
	ids := make([]string, 0, len(decision.Sessions))
	for i := range decision.Sessions {
		ids = append(ids, decision.Sessions[i].VideoID)
	}

	slices.Sort(ids)

	return ids
}

func checkFrozenScenario(t *testing.T, scenario *frozenScenario, frozen SessionState) {
	t.Helper()

	stored := scopePending(testVideoID, 800, endA().EffectiveAt)

	withPending, err := scenario.reduce(frozenBaseState(frozen, &stored, scenario.slots))
	if err != nil {
		t.Fatalf("reduce with frozen pending: %v", err)
	}

	withoutPending, err := scenario.reduce(frozenBaseState(frozen, nil, scenario.slots))
	if err != nil {
		t.Fatalf("reduce without frozen pending: %v", err)
	}

	for _, decision := range []*Decision{&withPending, &withoutPending} {
		if got := dirtyIDs(decision); !slices.Equal(got, scenario.dirty) {
			t.Fatalf("dirty sessions = %v, want %v", got, scenario.dirty)
		}
	}

	// 동결 pending은 결정에 그대로 실려 다시 쓰일 값이 같고, 없으면 새로 생기지 않는다.
	if got, ok := pendingByVideo(withPending.PendingEnds)[testVideoID]; !ok || !reflect.DeepEqual(got, stored) {
		t.Fatalf("frozen pending = %#v (present=%t), want %#v", got, ok, stored)
	}

	if _, ok := pendingByVideo(withoutPending.PendingEnds)[testVideoID]; ok {
		t.Fatal("frozen session gained a pending end")
	}

	// 동결 pending을 읽지 않은 상태도 나머지 결정이 같아야 loader가 그 행을 건너뛸 수 있다.
	withPending.PendingEnds = slices.DeleteFunc(withPending.PendingEnds, func(item PendingEnd) bool {
		return item.VideoID == testVideoID
	})

	sortScopeDecision(&withPending)
	sortScopeDecision(&withoutPending)

	if !reflect.DeepEqual(withPending, withoutPending) {
		t.Fatalf("frozen pending changed the decision\nwith=%+v\nwithout=%+v", withPending, withoutPending)
	}
}

// 종료 후보 없는 저장된 ENDED 세션은 어떤 사실·부재·due 정리로도 dirty가 되지 않고, 그 pending은 결정에
// 영향을 주지 않는다. 그래서 sourceobservation loader는 그 pending을 읽거나 잠그지 않으며, pending 삭제는
// dirty 세션으로 한정되므로 읽지 않은 행이 지워지지 않는다. 대조군 세션은 같은 호출에서 계속 부재를 세고
// due 후보를 정리한다.
func TestReduceNeverDirtiesFrozenEndedSession(t *testing.T) {
	t.Parallel()

	for variant, frozen := range frozenVariants() {
		for _, scenario := range frozenScenarios() {
			t.Run(variant+"/"+scenario.name, func(t *testing.T) {
				t.Parallel()

				checkFrozenScenario(t, &scenario, frozen)
			})
		}
	}
}
