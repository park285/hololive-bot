package live

import (
	"cmp"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	scopeUpcomingID = "up-1"
	scopeLiveID     = "live-1"
	scopeCountedID  = "live-2"
	scopeDueID      = "end-due"
)

var (
	scopeBase      = time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	scopeEndedFact = string(domain.LiveStatusEnded)
	scopeUpFact    = string(domain.LiveStatusUpcoming)
)

type scopeScenario struct {
	name    string
	payload []string
	reduce  func(State) (Decision, error)
}

func scopeObserved(videoID string, status domain.LiveStatus) SessionState {
	return SessionState{
		VideoID: videoID, ChannelID: channelCoverage().RequestedChannelIDs[0], Status: status,
		LifecycleOrigin: OriginObserved, LastSeenAt: scopeBase, Present: true, HeadPresent: true,
		IgnoredAbsences: LoadedIgnoredAbsences(nil),
	}
}

func scopeEnded(videoID string, history int) SessionState {
	session := scopeObserved(videoID, domain.LiveStatusEnded)
	slots := make([]time.Time, history)

	for i := range slots {
		slots[i] = scopeBase.Add(-time.Duration(i+3) * time.Minute)
	}

	session.StartedAt = new(scopeBase.Add(-2 * time.Hour))
	session.EndedAt = new(scopeBase.Add(-time.Hour))
	session.Clock.LastLivePositiveAt = new(scopeBase.Add(-2 * time.Hour))
	session.Clock.LastLivePositiveSeenAt = new(scopeBase.Add(-2 * time.Hour))
	session.Clock.EndedAt = session.EndedAt
	session.IgnoredAbsences = LoadedIgnoredAbsences(slots)

	return session
}

func scopePending(videoID string, observationID int64, at time.Time) PendingEnd {
	return PendingEnd{
		Kind: EndEvidenceExplicitEnd, VideoID: videoID, ChannelID: channelCoverage().RequestedChannelIDs[0],
		ObservationID: observationID, EffectiveAt: at, ReceivedAt: at, ScheduledFor: at,
		NegativeEligible: true, ScopeCovers: true,
	}
}

func addScopeActiveSessions(state *State) {
	upcoming := scopeObserved(scopeUpcomingID, domain.LiveStatusUpcoming)

	upcoming.Clock.LastUpcomingPositiveAt = new(scopeBase.Add(-time.Hour))
	upcoming.Clock.LastUpcomingPositiveSeenAt = new(scopeBase.Add(-time.Hour))
	upcoming.IgnoredAbsences = LoadedIgnoredAbsences([]time.Time{scopeBase.Add(-3 * time.Hour), scopeBase.Add(-2 * time.Hour)})
	state.Sessions[upcoming.VideoID] = upcoming

	for _, videoID := range []string{scopeLiveID, scopeCountedID} {
		session := scopeObserved(videoID, domain.LiveStatusLive)

		session.StartedAt = new(scopeBase)
		session.Clock.LastLivePositiveAt = new(scopeBase)
		session.Clock.LastLivePositiveSeenAt = new(scopeBase)
		state.Sessions[videoID] = session
	}

	counted := state.Sessions[scopeCountedID]

	counted.Clock.ConsecutiveAbsenceSlots = 1
	counted.FirstAbsenceScheduledFor = new(scopeBase.Add(30 * time.Minute))
	counted.LastAbsenceScheduledFor = new(scopeBase.Add(30 * time.Minute))
	counted.LastAbsenceObservationID = 703
	state.Sessions[scopeCountedID] = counted
}

// addScopeEndedSessions는 ENDED 세션을 채운다. 여기서 end-due만 due candidate를 가지고,
// end-fut는 아직 due가 아닌 candidate를 가진다.
func addScopeEndedSessions(state *State) {
	for i, videoID := range []string{"end-1", "end-2", "end-3", "end-4", "end-5"} {
		state.Sessions[videoID] = scopeEnded(videoID, 50*(i+1))
	}

	for _, candidate := range []struct {
		videoID       string
		observationID int64
		next          time.Time
	}{
		{scopeDueID, 900, scopeBase.Add(time.Hour)},
		{"end-fut", 901, scopeBase.Add(100 * 24 * time.Hour)},
	} {
		session := scopeEnded(candidate.videoID, 40)
		kind := EndEvidenceExplicitEnd

		session.Clock.EndCandidateKind = &kind
		session.Clock.EndCandidateObservationID = new(candidate.observationID)
		session.Clock.NextEndCheckAt = new(candidate.next)
		state.Sessions[candidate.videoID] = session
		state.PendingEnds[candidate.videoID] = scopePending(candidate.videoID, candidate.observationID, scopeBase)
	}

	state.PendingEnds["end-1"] = scopePending("end-1", 800, scopeBase.Add(-time.Hour))
}

// scopeFullState는 채널 전체를 이력까지 적재한 상태다.
func scopeFullState() State {
	state := State{Sessions: map[string]SessionState{}, PendingEnds: map[string]PendingEnd{}}

	addScopeActiveSessions(&state)
	addScopeEndedSessions(&state)

	for i := range 3 {
		at := scopeBase.Add(time.Duration(i+1) * 10 * time.Minute)

		state.AbsenceSlots = append(state.AbsenceSlots, AbsenceSlot{
			ObservationID: int64(701 + i), ScheduledFor: at, EffectiveAt: at, ReceivedAt: at, Coverage: channelCoverage(),
		})
	}

	return state
}

// narrowScopeState는 축소 적재 결과를 흉내 낸다. 읽지 않는 행은 payload 밖의 ENDED 중 candidate가
// 없는 세션과 그 pending이다(dueArm=false면 candidate가 있어도 뺀다). 남은 ENDED는 이력을 적재하지 않는다.
func narrowScopeState(full *State, payload []string, dueArm bool) (State, map[string]struct{}) {
	narrowed := full.clone()
	omitted := map[string]struct{}{}

	for videoID := range narrowed.Sessions {
		session := narrowed.Sessions[videoID]
		if !session.Present || session.Status != domain.LiveStatusEnded {
			continue
		}

		if !slices.Contains(payload, videoID) && (!dueArm || session.Clock.NextEndCheckAt == nil) {
			delete(narrowed.Sessions, videoID)
			delete(narrowed.PendingEnds, videoID)

			continue
		}

		session.IgnoredAbsences = IgnoredAbsenceHistory{}
		narrowed.Sessions[videoID] = session
		omitted[videoID] = struct{}{}
	}

	return narrowed, omitted
}

func sortScopeDecision(decision *Decision) {
	slices.SortFunc(decision.Sessions, func(a, b SessionState) int { return cmp.Compare(a.VideoID, b.VideoID) })
	slices.SortFunc(decision.PendingEnds, func(a, b PendingEnd) int { return cmp.Compare(a.VideoID, b.VideoID) })
	slices.SortStableFunc(decision.Applications, func(a, b Application) int {
		return cmp.Or(cmp.Compare(a.EntityKey, b.EntityKey), cmp.Compare(a.Decision, b.Decision))
	})
}

func requireEquivalentScopeSessions(t *testing.T, fullDecision, narrowedDecision *Decision, omitted map[string]struct{}) {
	t.Helper()

	if len(fullDecision.Sessions) != len(narrowedDecision.Sessions) {
		t.Fatalf("dirty sessions full=%d narrowed=%d", len(fullDecision.Sessions), len(narrowedDecision.Sessions))
	}

	for i := range fullDecision.Sessions {
		want, got := fullDecision.Sessions[i], narrowedDecision.Sessions[i]

		if _, ok := omitted[got.VideoID]; ok {
			// 이력 없이 읽은 ENDED는 저장 때 기존 배열을 유지하도록 미적재로 남아야 한다.
			if _, loaded := got.IgnoredAbsences.Slots(); loaded {
				t.Fatalf("%s: omitted history became loaded", got.VideoID)
			}

			got.IgnoredAbsences = want.IgnoredAbsences
		}

		if !reflect.DeepEqual(want, got) {
			t.Fatalf("dirty session %s differs\nfull=%+v\nnarrowed=%+v", want.VideoID, want, got)
		}
	}
}

func requireEquivalentScopeDecision(t *testing.T, full *State, fullDecision, narrowedDecision Decision, omitted map[string]struct{}) {
	t.Helper()

	sortScopeDecision(&fullDecision)
	sortScopeDecision(&narrowedDecision)
	requireEquivalentScopeSessions(t, &fullDecision, &narrowedDecision, omitted)

	if !reflect.DeepEqual(fullDecision.Applications, narrowedDecision.Applications) {
		t.Fatalf("applications differ\nfull=%+v\nnarrowed=%+v", fullDecision.Applications, narrowedDecision.Applications)
	}

	if !reflect.DeepEqual(fullDecision.AbsenceSlot, narrowedDecision.AbsenceSlot) {
		t.Fatalf("absence slot differs\nfull=%+v\nnarrowed=%+v", fullDecision.AbsenceSlot, narrowedDecision.AbsenceSlot)
	}

	fullPending := map[string]PendingEnd{}

	for i := range fullDecision.PendingEnds {
		fullPending[fullDecision.PendingEnds[i].VideoID] = fullDecision.PendingEnds[i]
	}

	for i := range narrowedDecision.PendingEnds {
		pending := &narrowedDecision.PendingEnds[i]
		if !reflect.DeepEqual(fullPending[pending.VideoID], *pending) {
			t.Fatalf("pending %s differs", pending.VideoID)
		}

		delete(fullPending, pending.VideoID)
	}

	// full에만 있는 pending은 축소 적재가 읽지 않은 행이며 reducer도 바꾸지 않아야 한다.
	for videoID := range fullPending {
		if !reflect.DeepEqual(full.PendingEnds[videoID], fullPending[videoID]) {
			t.Fatalf("unloaded pending %s was changed by the full reducer", videoID)
		}
	}
}

func dirtySession(decision *Decision, videoID string) (SessionState, bool) {
	for i := range decision.Sessions {
		if decision.Sessions[i].VideoID == videoID {
			return decision.Sessions[i], true
		}
	}

	return SessionState{}, false
}

func scopeScenarios() []scopeScenario {
	at := scopeBase.Add(2 * time.Hour)
	ended := scopeBase.Add(-time.Hour)
	started := scopeBase.Add(90 * time.Minute)
	channel := channelCoverage().RequestedChannelIDs[0]
	snapshot := func(id int64, completeness contract.Completeness, continuity contract.Continuity, facts ...SessionFact) Evidence {
		return liveEvidence(id, at, completeness, continuity, facts...)
	}
	reduceWith := func(evidence Evidence) func(State) (Decision, error) {
		return func(state State) (Decision, error) { return Reduce(state, evidence, time.Hour, at) }
	}

	return []scopeScenario{
		{
			name:    "youtubejs complete with past ended",
			payload: []string{"end-1", "end-2", scopeLiveID, "new-1"},
			reduce: reduceWith(snapshot(1001, contract.CompletenessComplete, contract.ContinuityContiguous,
				SessionFact{VideoID: "end-1", ChannelID: channel, Status: scopeEndedFact, EndedAt: &ended},
				SessionFact{VideoID: "end-2", ChannelID: channel, Status: scopeEndedFact, EndedAt: &ended},
				SessionFact{VideoID: scopeLiveID, ChannelID: channel, Status: testLiveStatus, LiveStartConfirmed: true},
				SessionFact{VideoID: "new-1", ChannelID: channel, Status: scopeUpFact},
			)),
		},
		{
			name:    "holodex partial late live",
			payload: []string{scopeUpcomingID, "end-3", "new-2"},
			reduce: reduceWith(snapshot(1002, contract.CompletenessPartial, contract.ContinuityNotApplicable,
				SessionFact{VideoID: scopeUpcomingID, ChannelID: channel, Status: scopeUpFact},
				SessionFact{VideoID: "end-3", ChannelID: channel, Status: testLiveStatus, StartedAt: &started, LiveStartConfirmed: true},
				SessionFact{VideoID: "new-2", ChannelID: channel, Status: testLiveStatus, StartedAt: &started, LiveStartConfirmed: true},
			)),
		},
		{
			name:   "complete empty",
			reduce: reduceWith(snapshot(1003, contract.CompletenessComplete, contract.ContinuityContiguous)),
		},
		{
			name:    "video live check",
			payload: []string{scopeLiveID},
			reduce: reduceWith(videoCheckEvidence(1004, at,
				SessionFact{VideoID: scopeLiveID, ChannelID: channel, Status: testLiveStatus, LiveStartConfirmed: true})),
		},
		{
			name:    "video live check on ended",
			payload: []string{scopeDueID},
			reduce: reduceWith(videoCheckEvidence(1005, at,
				SessionFact{VideoID: scopeDueID, ChannelID: channel, Status: scopeEndedFact, EndedAt: &ended, VerifiedTerminal: true})),
		},
		{
			name: "finalize due",
			reduce: func(state State) (Decision, error) {
				return FinalizeDue(state, at, time.Hour), nil
			},
		},
	}
}

func checkScopeScenario(t *testing.T, scenario *scopeScenario) {
	t.Helper()

	full := scopeFullState()
	narrowed, omitted := narrowScopeState(&full, scenario.payload, true)

	fullDecision, err := scenario.reduce(full)
	if err != nil {
		t.Fatalf("full reduce: %v", err)
	}

	narrowedDecision, err := scenario.reduce(narrowed)
	if err != nil {
		t.Fatalf("narrowed reduce: %v", err)
	}

	requireEquivalentScopeDecision(t, &full, fullDecision, narrowedDecision, omitted)

	// 대조군: due candidate가 남은 ENDED는 payload 밖이어도 정리되어 저장 대상이 된다.
	due, ok := dirtySession(&fullDecision, scopeDueID)
	if !ok || due.Clock.NextEndCheckAt != nil || due.Clock.EndCandidateKind != nil {
		t.Fatalf("due candidate on ENDED was not cleared: ok=%t %+v", ok, due.Clock)
	}

	if slices.Contains(scenario.payload, scopeDueID) {
		return
	}

	withoutDueArm, _ := narrowScopeState(&full, scenario.payload, false)

	controlDecision, err := scenario.reduce(withoutDueArm)
	if err != nil {
		t.Fatalf("control reduce: %v", err)
	}

	if _, ok := dirtySession(&controlDecision, scopeDueID); ok {
		t.Fatal("control unexpectedly settled a session it did not load")
	}
}

// 축소 적재(채널 범위 ENDED 제외, ENDED 이력 미적재)는 reducer 결정을 바꾸지 않는다.
// 대조군으로, due candidate가 남은 ENDED를 함께 읽지 않으면 candidate 정리가 빠지는지도 확인한다.
func TestReduceNarrowedLiveStateMatchesFullState(t *testing.T) {
	t.Parallel()

	for _, scenario := range scopeScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			checkScopeScenario(t, &scenario)
		})
	}
}

// 부재 적용에 필요한 이력을 적재하지 않았으면 부분 결정 없이 오류로 드러낸다. ENDED는 이력을 읽지 않는다.
func TestReduceRequiresLoadedIgnoredAbsencesOnlyWhenRead(t *testing.T) {
	t.Parallel()

	full := scopeFullState()
	evidence := absenceAt(2001, scopeBase.Add(2*time.Hour))

	for _, videoID := range []string{scopeUpcomingID, scopeLiveID} {
		t.Run(videoID, func(t *testing.T) {
			t.Parallel()

			state := full.clone()
			session := state.Sessions[videoID]

			session.IgnoredAbsences = IgnoredAbsenceHistory{}
			state.Sessions[videoID] = session

			decision, err := Reduce(state, evidence, time.Hour, evidence.ReceivedAt)
			if !errors.Is(err, errIgnoredAbsencesNotLoaded) {
				t.Fatalf("err = %v, want %v", err, errIgnoredAbsencesNotLoaded)
			}

			if !reflect.DeepEqual(decision, Decision{}) {
				t.Fatalf("failed reduce returned a partial decision: %+v", decision)
			}
		})
	}

	state, _ := narrowScopeState(&full, nil, true)
	if _, err := Reduce(state, evidence, time.Hour, evidence.ReceivedAt); err != nil {
		t.Fatalf("ENDED sessions without loaded history must not fail: %v", err)
	}
}

func TestIgnoredAbsenceHistoryLoadedStateAndCloneIndependence(t *testing.T) {
	t.Parallel()

	var zero SessionState

	if slots, loaded := zero.IgnoredAbsences.Slots(); loaded || slots != nil {
		t.Fatalf("zero value = (%v, %t), want not loaded", slots, loaded)
	}

	empty := LoadedIgnoredAbsences(nil)
	if slots, loaded := empty.Slots(); !loaded || len(slots) != 0 {
		t.Fatalf("loaded empty = (%v, %t), want loaded empty", slots, loaded)
	}

	zeroClone := zero.clone()
	if _, loaded := zeroClone.IgnoredAbsences.Slots(); loaded {
		t.Fatal("clone loaded a history that was not loaded")
	}

	first := scopeBase.Add(-time.Hour)
	backing := []time.Time{first}
	original := SessionState{VideoID: "loaded", IgnoredAbsences: LoadedIgnoredAbsences(backing)}
	cloned := original.clone()

	backing[0] = scopeBase

	cloned.IgnoredAbsences.add(scopeBase.Add(time.Hour))

	if slots, _ := cloned.IgnoredAbsences.Slots(); len(slots) != 2 || !slots[0].Equal(first) {
		t.Fatalf("clone shares storage with the original: %v", slots)
	}

	if slots, _ := original.IgnoredAbsences.Slots(); len(slots) != 1 {
		t.Fatalf("clone append changed the original: %v", slots)
	}
}

// Reduce가 무시한 부재를 추가해도 호출자 상태의 여유 용량에 쓰지 않는다.
func TestReduceDoesNotWriteCallerIgnoredAbsenceStorage(t *testing.T) {
	t.Parallel()

	spare := make([]time.Time, 0, 4)
	state := scopeFullState()
	upcoming := state.Sessions[scopeUpcomingID]

	upcoming.IgnoredAbsences = LoadedIgnoredAbsences(spare)
	state.Sessions[scopeUpcomingID] = upcoming

	evidence := absenceAt(2002, scopeBase.Add(2*time.Hour))

	decision, err := Reduce(state, evidence, time.Hour, evidence.ReceivedAt)
	if err != nil {
		t.Fatal(err)
	}

	session, ok := dirtySession(&decision, scopeUpcomingID)
	if !ok {
		t.Fatal("absence before LIVE positive was not recorded")
	}

	if slots, loaded := session.IgnoredAbsences.Slots(); !loaded || len(slots) == 0 {
		t.Fatalf("reduced history = (%v, %t)", slots, loaded)
	}

	if !spare[:1][0].IsZero() {
		t.Fatal("Reduce wrote into the caller's history storage")
	}
}
