package live

import (
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type reduceSession struct {
	state        *State
	evidence     *Evidence
	grace        time.Duration
	dbNow        time.Time
	dirty        map[string]struct{}
	applications []Application
}

func Reduce(state State, evidence Evidence, grace time.Duration, dbNow time.Time) (Decision, error) {
	if evidence.Kind != contract.KindLiveSnapshot {
		return Decision{}, fmt.Errorf("live reducer received kind %q", evidence.Kind)
	}

	workingState := state.clone()
	workingEvidence := evidence.clone()
	session := reduceSession{
		state:    &workingState,
		evidence: &workingEvidence,
		grace:    grace,
		dbNow:    dbNow.UTC(),
		dirty:    map[string]struct{}{},
	}

	if session.state.Sessions == nil {
		session.state.Sessions = map[string]SessionState{}
	}

	if session.state.PendingEnds == nil {
		session.state.PendingEnds = map[string]PendingEnd{}
	}

	applyFacts(&session)

	return session.decision(), nil
}

func applyFacts(session *reduceSession) {
	seen := map[string]SessionFact{}

	for i := range session.evidence.Sessions {
		fact := &session.evidence.Sessions[i]

		seen[fact.VideoID] = *fact
		applySessionFact(session, fact)
	}

	if scopedNegative(session.evidence) {
		applySnapshotAbsence(session, seen)
	}

	reapplyStoredAbsences(session, seen)

	for videoID := range session.state.Sessions {
		reapplyStoredEnds(session, videoID)
		settleDueCandidate(session, videoID)
	}
}

func applySessionFact(session *reduceSession, fact *SessionFact) {
	if fact.Status == "UPCOMING" {
		applyUpcomingPositive(session, fact)

		return
	}

	if fact.Status == "LIVE" {
		applyLivePositive(session, fact)

		return
	}

	applyNegativeFact(session, fact)
}

func applyNegativeFact(session *reduceSession, fact *SessionFact) {
	if fact.Status == "ENDED" {
		pending := pendingFromFact(session, fact, EndEvidenceExplicitEnd)
		recordPendingEnd(session, &pending)
		reapplyStoredEnds(session, fact.VideoID)

		return
	}

	if fact.Status == "CANCELLED" { //nolint:misspell // YouTube 방송 상태 계약값이 영국식 CANCELLED라, canceled로 바꾸면 상태 판정이 어긋난다.
		pending := pendingFromFact(session, fact, EndEvidenceExplicitCancel)
		recordPendingEnd(session, &pending)
		reapplyStoredEnds(session, fact.VideoID)
	}
}

func scopedNegative(evidence *Evidence) bool {
	return contract.NegativeEligible(evidence.Completeness, evidence.Continuity) &&
		contract.AbsenceCapabilityFor(evidence.Kind) == contract.AbsenceScoped
}

func applySnapshotAbsence(session *reduceSession, seen map[string]SessionFact) {
	slot, replay := recordAbsenceSlot(session)
	if replay {
		return
	}

	for videoID := range session.state.Sessions {
		if _, present := seen[videoID]; present {
			continue
		}

		existing := session.state.Sessions[videoID]
		applyAbsenceToSession(session, &existing, &slot)
	}
}

func recordAbsenceSlot(session *reduceSession) (AbsenceSlot, bool) {
	evidence := session.evidence
	for i := range session.state.AbsenceSlots {
		if session.state.AbsenceSlots[i].ScheduledFor.Equal(evidence.ScheduledFor) {
			return session.state.AbsenceSlots[i], true
		}
	}

	slot := AbsenceSlot{
		ScheduledFor:   evidence.ScheduledFor,
		ObservationID:  evidence.ObservationID,
		EvidenceSHA256: evidence.EvidenceSHA256,
		EffectiveAt:    evidence.EffectiveAt,
		ReceivedAt:     evidence.ReceivedAt,
		ScopeSHA256:    evidence.ScopeSHA256,
		Coverage:       evidence.Coverage,
	}

	session.state.AbsenceSlots = append(session.state.AbsenceSlots, slot)

	return slot, false
}

func (s *reduceSession) decision() Decision {
	sessions := make([]SessionState, 0, len(s.dirty))
	for videoID := range s.dirty {
		if state, ok := s.state.Sessions[videoID]; ok {
			sessions = append(sessions, state)
		}
	}

	pending := make([]PendingEnd, 0, len(s.state.PendingEnds))
	for videoID := range s.state.PendingEnds {
		fact := s.state.PendingEnds[videoID]

		pending = append(pending, fact)
	}

	return Decision{
		Sessions:     sessions,
		PendingEnds:  pending,
		AbsenceSlot:  absenceSlotOf(s.state, s.evidence),
		Applications: boundApplications(s.applications),
	}
}

func absenceSlotOf(state *State, evidence *Evidence) *AbsenceSlot {
	if !scopedNegative(evidence) {
		return nil
	}

	for i := range state.AbsenceSlots {
		if state.AbsenceSlots[i].ScheduledFor.Equal(evidence.ScheduledFor) {
			slot := state.AbsenceSlots[i]
			return &slot
		}
	}

	return nil
}

func FinalizeDue(state State, dbNow time.Time, grace time.Duration) Decision {
	workingState := state.clone()
	workingEvidence := Evidence{}
	session := reduceSession{
		state:    &workingState,
		evidence: &workingEvidence,
		grace:    grace,
		dbNow:    dbNow.UTC(),
		dirty:    map[string]struct{}{},
	}

	if session.state.Sessions == nil {
		session.state.Sessions = map[string]SessionState{}
	}

	if session.state.PendingEnds == nil {
		session.state.PendingEnds = map[string]PendingEnd{}
	}

	for videoID := range session.state.Sessions {
		reapplyStoredEnds(&session, videoID)
		settleDueCandidate(&session, videoID)
	}

	return session.decision()
}

func boundApplications(items []Application) []Application {
	if len(items) <= 1000 {
		return items
	}

	return items[:1000]
}

func markDirty(session *reduceSession, videoID string) {
	session.dirty[videoID] = struct{}{}
}

func copyTime(value time.Time) *time.Time {
	copied := value.UTC()
	return &copied
}

func copyOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	copied := value.UTC()

	return &copied
}
