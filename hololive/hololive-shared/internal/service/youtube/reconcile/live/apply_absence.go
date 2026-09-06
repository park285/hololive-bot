package live

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func pendingFromFact(session *reduceSession, fact *SessionFact, kind EndEvidenceKind) PendingEnd {
	return PendingEnd{
		Kind:             kind,
		VideoID:          fact.VideoID,
		ChannelID:        fact.ChannelID,
		ObservationID:    session.evidence.ObservationID,
		EffectiveAt:      session.evidence.EffectiveAt,
		ReceivedAt:       session.evidence.ReceivedAt,
		ScheduledFor:     session.evidence.ScheduledFor,
		EndedAt:          copyOptionalTime(fact.EndedAt),
		NegativeEligible: true,
		ScopeCovers:      true,
	}
}

func recordPendingEnd(session *reduceSession, pending *PendingEnd) {
	existing, ok := session.state.PendingEnds[pending.VideoID]
	if ok && pending.EffectiveAt.Before(existing.EffectiveAt) {
		return
	}

	session.state.PendingEnds[pending.VideoID] = *pending
}

func reapplyStoredAbsences(session *reduceSession, seen map[string]SessionFact) {
	for i := range session.state.AbsenceSlots {
		slot := &session.state.AbsenceSlots[i]
		for videoID := range session.state.Sessions {
			if seenInCurrentSlot(session, slot, videoID, seen) {
				continue
			}

			existing := session.state.Sessions[videoID]
			applyAbsenceToSession(session, &existing, slot)
		}
	}
}

func seenInCurrentSlot(
	session *reduceSession,
	slot *AbsenceSlot,
	videoID string,
	seen map[string]SessionFact,
) bool {
	if !slot.ScheduledFor.Equal(session.evidence.ScheduledFor) {
		return false
	}

	_, present := seen[videoID]

	return present
}

func applyAbsenceToSession(session *reduceSession, existing *SessionState, slot *AbsenceSlot) {
	if session == nil || session.state == nil || existing == nil || slot == nil {
		return
	}

	applyAbsenceToKnownSession(session, existing, slot)
}

func applyAbsenceToKnownSession(session *reduceSession, existing *SessionState, slot *AbsenceSlot) {
	if existing.Status == StatusEnded || !existing.Present {
		return
	}

	covers := contract.LiveCoverageCoversSession(slot.Coverage, existing.ChannelID, string(existing.Status))
	if !covers {
		return
	}

	ignoredSlots := ignoredAbsenceSlots(session, existing)
	if _, ignored := ignoredSlots[slot.ScheduledFor.UTC()]; ignored {
		return
	}

	if existing.Clock.LastLivePositiveAt == nil {
		existing.IgnoredAbsenceScheduledFor = append(existing.IgnoredAbsenceScheduledFor, slot.ScheduledFor)
		ignoredSlots[slot.ScheduledFor.UTC()] = struct{}{}
		session.state.Sessions[existing.VideoID] = *existing
		markDirty(session, existing.VideoID)

		return
	}

	applyAbsenceAfterPositive(session, existing, slot, covers)
}

func applyAbsenceAfterPositive(session *reduceSession, existing *SessionState, slot *AbsenceSlot, covers bool) {
	if slot.EffectiveAt.After(*existing.Clock.LastLivePositiveAt) && !replayedAbsence(existing, slot) {
		recordAbsencePending(session, existing, slot, covers)
	}
}

func recordAbsencePending(session *reduceSession, existing *SessionState, slot *AbsenceSlot, covers bool) {
	countAbsenceSlot(existing, slot)

	existing.Clock.LastCompleteAbsenceAt = copyTime(slot.EffectiveAt)
	existing.LastAbsenceObservationID = slot.ObservationID
	existing.LastAbsenceScheduledFor = copyTime(slot.ScheduledFor)
	session.state.Sessions[existing.VideoID] = *existing
	markDirty(session, existing.VideoID)

	pending := PendingEnd{
		Kind:             EndEvidenceScopedAbsence,
		VideoID:          existing.VideoID,
		ChannelID:        existing.ChannelID,
		ObservationID:    slot.ObservationID,
		EffectiveAt:      slot.EffectiveAt,
		ReceivedAt:       slot.ReceivedAt,
		ScheduledFor:     slot.ScheduledFor,
		NegativeEligible: true,
		ScopeCovers:      covers,
	}
	recordPendingEnd(session, &pending)
	reapplyStoredEnds(session, existing.VideoID)
}

func ignoredAbsenceSlots(session *reduceSession, existing *SessionState) map[time.Time]struct{} {
	if session.ignoredAbsences == nil {
		session.ignoredAbsences = make(map[string]map[time.Time]struct{})
	}

	ignored := session.ignoredAbsences[existing.VideoID]
	if ignored == nil {
		// 신규 positive가 긴 이력을 복원할 때 같은 목록을 slot마다 선형 탐색하지 않는다.
		ignored = make(map[time.Time]struct{}, len(existing.IgnoredAbsenceScheduledFor))
		for _, at := range existing.IgnoredAbsenceScheduledFor {
			ignored[at.UTC()] = struct{}{}
		}

		session.ignoredAbsences[existing.VideoID] = ignored
	}

	return ignored
}

func replayedAbsence(existing *SessionState, slot *AbsenceSlot) bool {
	if existing == nil || slot == nil {
		return true
	}

	if slot.ObservationID != 0 && existing.LastAbsenceObservationID == slot.ObservationID {
		return true
	}

	return sameOptionalTime(existing.FirstAbsenceScheduledFor, &slot.ScheduledFor) ||
		sameOptionalTime(existing.SecondAbsenceScheduledFor, &slot.ScheduledFor)
}

func countAbsenceSlot(entity *SessionState, slot *AbsenceSlot) {
	if entity == nil || slot == nil {
		return
	}

	switch entity.Clock.ConsecutiveAbsenceSlots {
	case 0:
		entity.FirstAbsenceScheduledFor = copyTime(slot.ScheduledFor)
		entity.Clock.ConsecutiveAbsenceSlots = 1
	case 1:
		entity.SecondAbsenceScheduledFor = copyTime(slot.ScheduledFor)
		entity.Clock.ConsecutiveAbsenceSlots = 2
	default:
	}
}
