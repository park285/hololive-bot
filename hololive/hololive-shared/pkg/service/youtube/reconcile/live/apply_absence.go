package live

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func pendingFromFact(session *reduceSession, fact SessionFact, kind EndEvidenceKind) PendingEnd {
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

func recordPendingEnd(session *reduceSession, pending PendingEnd) {
	existing, ok := session.state.PendingEnds[pending.VideoID]
	if ok && pending.EffectiveAt.Before(existing.EffectiveAt) {
		return
	}
	session.state.PendingEnds[pending.VideoID] = pending
}

func reapplyStoredAbsences(session *reduceSession) {
	for _, slot := range session.state.AbsenceSlots {
		for _, existing := range session.state.Sessions {
			applyAbsenceToSession(session, existing, slot)
		}
	}
}

func applyAbsenceToSession(session *reduceSession, existing SessionState, slot AbsenceSlot) {
	if existing.Status == StatusEnded || !existing.Present {
		return
	}
	covers := contract.LiveCoverageCoversSession(slot.Coverage, existing.ChannelID, string(existing.Status))
	if !covers || ignoredAbsence(existing, slot.ScheduledFor) {
		return
	}
	if existing.Clock.LastLivePositiveAt == nil {
		existing.IgnoredAbsenceScheduledFor = append(existing.IgnoredAbsenceScheduledFor, slot.ScheduledFor)
		session.state.Sessions[existing.VideoID] = existing
		markDirty(session, existing.VideoID)
		return
	}
	if !slot.EffectiveAt.After(*existing.Clock.LastLivePositiveAt) || replayedAbsence(existing, slot) {
		return
	}
	recordAbsencePending(session, existing, slot, covers)
}

func recordAbsencePending(session *reduceSession, existing SessionState, slot AbsenceSlot, covers bool) {
	countAbsenceSlot(&existing, slot)
	existing.Clock.LastCompleteAbsenceAt = copyTime(slot.EffectiveAt)
	existing.LastAbsenceObservationID = slot.ObservationID
	existing.LastAbsenceScheduledFor = copyTime(slot.ScheduledFor)
	session.state.Sessions[existing.VideoID] = existing
	markDirty(session, existing.VideoID)
	recordPendingEnd(session, PendingEnd{
		Kind:             EndEvidenceScopedAbsence,
		VideoID:          existing.VideoID,
		ChannelID:        existing.ChannelID,
		ObservationID:    slot.ObservationID,
		EffectiveAt:      slot.EffectiveAt,
		ReceivedAt:       slot.ReceivedAt,
		ScheduledFor:     slot.ScheduledFor,
		NegativeEligible: true,
		ScopeCovers:      covers,
	})
	reapplyStoredEnds(session, existing.VideoID)
}

func ignoredAbsence(existing SessionState, scheduledFor time.Time) bool {
	for _, ignored := range existing.IgnoredAbsenceScheduledFor {
		if ignored.Equal(scheduledFor) {
			return true
		}
	}
	return false
}

func replayedAbsence(existing SessionState, slot AbsenceSlot) bool {
	if slot.ObservationID != 0 && existing.LastAbsenceObservationID == slot.ObservationID {
		return true
	}
	return sameOptionalTime(existing.FirstAbsenceScheduledFor, &slot.ScheduledFor) ||
		sameOptionalTime(existing.SecondAbsenceScheduledFor, &slot.ScheduledFor)
}

func countAbsenceSlot(entity *SessionState, slot AbsenceSlot) {
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
