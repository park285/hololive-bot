package content

import "time"

const entityKindVideo = "youtube_video"

func applyPositives(session *reduceSession) {
	for i := range session.evidence.Videos {
		applyPositive(session, &session.evidence.Videos[i])
	}
}

func applyPositive(session *reduceSession, entity *Entity) {
	existing, ok := session.state.Videos[entity.VideoID]
	if !ok {
		session.state.Videos[entity.VideoID] = newEntityState(entity, session.evidence)
		markApplied(session, *entity)

		session.applications = append(session.applications, Application{
			EntityKind: entityKindVideo, EntityKey: entity.VideoID, Decision: "APPLIED",
		})
		applyStoredNegatives(session, entity.VideoID)

		return
	}

	if session.evidence.EffectiveAt.Before(existing.Clock.LastPositiveEffectiveAt) {
		applyOlderPositive(session, &existing, entity)

		return
	}

	if session.evidence.EffectiveAt.Equal(existing.Clock.LastPositiveEffectiveAt) {
		applyEqualTimePositive(session, &existing, entity)

		return
	}

	applyNewerPositive(session, &existing, entity)
}

func newEntityState(entity *Entity, evidence *Evidence) EntityState {
	digest := valueDigest(entity)

	return EntityState{
		Entity:                   *entity,
		FirstPositiveEffectiveAt: evidence.EffectiveAt,
		LastPositiveValueSHA256:  digest,
		LastPositiveScopeSHA256:  evidence.ScopeSHA256,
		LastPositiveCoverage:     evidence.Coverage,
		Clock: ContentEvidenceClock{
			LastPositiveEffectiveAt: evidence.EffectiveAt,
			LastPositiveReceivedAt:  evidence.ReceivedAt,
		},
	}
}

func applyOlderPositive(session *reduceSession, existing *EntityState, entity *Entity) {
	if session.evidence.EffectiveAt.Before(existing.FirstPositiveEffectiveAt) {
		existing.FirstPositiveEffectiveAt = session.evidence.EffectiveAt
	}

	session.state.Videos[entity.VideoID] = *existing
	session.applications = append(session.applications, Application{
		EntityKind: entityKindVideo, EntityKey: entity.VideoID, Decision: "OLDER_POSITIVE_RETAINED",
	})
}

func applyEqualTimePositive(session *reduceSession, existing *EntityState, entity *Entity) {
	digest := valueDigest(entity)
	if digest != existing.LastPositiveValueSHA256 {
		session.conflicts = append(session.conflicts, Conflict{
			VideoID:              entity.VideoID,
			FieldName:            "content",
			ExistingValueSHA256:  existing.LastPositiveValueSHA256,
			AttemptedValueSHA256: digest,
		})
		session.applications = append(session.applications, Application{
			EntityKind: entityKindVideo, EntityKey: entity.VideoID, Decision: "CONFLICT_KEEP",
		})

		return
	}

	clearMissingIfDue(existing, session.evidence.EffectiveAt)
	commitPositive(session, existing)

	session.applications = append(session.applications, Application{
		EntityKind: entityKindVideo, EntityKey: entity.VideoID, Decision: "REPLAY",
	})
}

func applyNewerPositive(session *reduceSession, existing *EntityState, entity *Entity) {
	digest := valueDigest(entity)
	if digest != existing.LastPositiveValueSHA256 {
		session.fieldUpdates = append(session.fieldUpdates, *entity)
	}

	existing.Entity = *entity
	existing.LastPositiveValueSHA256 = digest
	existing.LastPositiveScopeSHA256 = session.evidence.ScopeSHA256
	existing.LastPositiveCoverage = session.evidence.Coverage
	existing.Clock.LastPositiveEffectiveAt = session.evidence.EffectiveAt
	existing.Clock.LastPositiveReceivedAt = session.evidence.ReceivedAt
	clearMissingIfDue(existing, session.evidence.EffectiveAt)
	commitPositive(session, existing)

	session.applications = append(session.applications, Application{
		EntityKind: entityKindVideo, EntityKey: entity.VideoID, Decision: "APPLIED",
	})
}

func commitPositive(session *reduceSession, entity *EntityState) {
	session.state.Videos[entity.VideoID] = *entity
	applyStoredNegatives(session, entity.VideoID)
}

func clearMissingIfDue(entity *EntityState, positiveAt time.Time) {
	if shouldClearMissing(entity, positiveAt) {
		clearMissing(entity)
	}
}

func shouldClearMissing(entity *EntityState, positiveAt time.Time) bool {
	cutoff := entity.Clock.LastNegativeEffectiveAt
	if cutoff == nil {
		cutoff = entity.Clock.MissingSinceEffectiveAt
	}

	if cutoff == nil {
		return false
	}

	return !positiveAt.Before(*cutoff)
}

func clearMissing(entity *EntityState) {
	entity.Clock.LastNegativeEffectiveAt = nil
	entity.LastNegativeReceivedAt = nil
	entity.Clock.MissingSinceEffectiveAt = nil
	entity.FirstAbsenceScheduledFor = nil
	entity.SecondAbsenceScheduledFor = nil
	entity.LastAbsenceObservationID = 0
	entity.ConsecutiveAbsenceSlots = 0
	entity.WithdrawnAt = nil
}
