package content

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func applyNegative(session *reduceSession) {
	slot, replay := recordAbsenceSlot(session)
	for videoID := range session.state.Videos {
		entity := session.state.Videos[videoID]
		applyNegativeToEntity(session, videoID, &entity, &slot, replay)
	}
}

func recordAbsenceSlot(session *reduceSession) (AbsenceSlot, bool) {
	evidence := session.evidence
	for i := range session.state.AbsenceSlots {
		existing := session.state.AbsenceSlots[i]
		if existing.ScheduledFor.Equal(evidence.ScheduledFor) {
			return existing, true
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

func applyStoredNegatives(session *reduceSession, videoID string) {
	entity, ok := session.state.Videos[videoID]
	if !ok {
		return
	}

	for i := range session.state.AbsenceSlots {
		slot := &session.state.AbsenceSlots[i]
		if !slot.EffectiveAt.After(entity.Clock.LastPositiveEffectiveAt) {
			continue
		}

		entity = session.state.Videos[videoID]
		applyNegativeToEntity(session, videoID, &entity, slot, false)
	}
}

func applyNegativeToEntity(
	session *reduceSession,
	videoID string,
	entity *EntityState,
	slot *AbsenceSlot,
	replay bool,
) {
	if session == nil || session.state == nil || entity == nil || slot == nil {
		return
	}

	applyNegativeToEntityChecked(session, videoID, entity, slot, replay)
}

func applyNegativeToEntityChecked(
	session *reduceSession,
	videoID string,
	entity *EntityState,
	slot *AbsenceSlot,
	replay bool,
) {
	if entity.LastPositiveValueSHA256 == "" {
		return
	}

	if !slot.Coverage.covers(entity.Entity) {
		return
	}

	if !contract.CoverageAllowsAbsence(entity.LastPositiveCoverage.relationTo(slot.Coverage)) {
		return
	}

	if !slot.EffectiveAt.After(entity.Clock.LastPositiveEffectiveAt) {
		return
	}

	if replayedNegative(entity, slot, replay) {
		return
	}

	recordMissing(entity, slot, session.grace)

	session.state.Videos[videoID] = *entity
}

func replayedNegative(entity *EntityState, slot *AbsenceSlot, replay bool) bool {
	if replay || entity == nil || slot == nil {
		return true
	}

	if slot.ObservationID != 0 && entity.LastAbsenceObservationID == slot.ObservationID {
		return true
	}

	if sameOptionalTime(entity.FirstAbsenceScheduledFor, &slot.ScheduledFor) {
		return true
	}

	return sameOptionalTime(entity.SecondAbsenceScheduledFor, &slot.ScheduledFor)
}

func recordMissing(entity *EntityState, slot *AbsenceSlot, grace time.Duration) {
	if entity == nil || slot == nil {
		return
	}

	effective := slot.EffectiveAt

	entity.Clock.LastNegativeEffectiveAt = &effective

	received := slot.ReceivedAt

	entity.LastNegativeReceivedAt = &received
	entity.LastAbsenceObservationID = slot.ObservationID

	if entity.Clock.MissingSinceEffectiveAt == nil {
		entity.Clock.MissingSinceEffectiveAt = &effective
	}

	countAbsenceSlot(entity, slot.ScheduledFor)

	if entity.ConsecutiveAbsenceSlots >= 2 && graceElapsed(entity, slot, grace) {
		withdrawn := slot.EffectiveAt

		entity.WithdrawnAt = &withdrawn
	}
}

func countAbsenceSlot(entity *EntityState, scheduledFor time.Time) {
	if entity == nil {
		return
	}

	switch entity.ConsecutiveAbsenceSlots {
	case 0:
		copied := scheduledFor

		entity.FirstAbsenceScheduledFor = &copied
		entity.ConsecutiveAbsenceSlots = 1
	case 1:
		copied := scheduledFor

		entity.SecondAbsenceScheduledFor = &copied
		entity.ConsecutiveAbsenceSlots = 2
	default:
	}
}

func graceElapsed(entity *EntityState, slot *AbsenceSlot, grace time.Duration) bool {
	if entity == nil || slot == nil {
		return false
	}

	return !slot.ReceivedAt.Before(entity.Clock.LastPositiveReceivedAt.Add(grace))
}

func sameOptionalTime(value, other *time.Time) bool {
	if value == nil || other == nil {
		return false
	}

	return value.Equal(*other)
}
