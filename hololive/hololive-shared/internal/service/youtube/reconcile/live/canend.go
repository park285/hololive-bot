package live

import "time"

func CanEnd(state *LiveEvidenceClock, evidence *EndEvidence, dbNow time.Time, grace time.Duration) bool {
	if !evidence.Valid || !evidence.EntityMatchesSession || evidence.HasPositiveAtOrAfter {
		return false
	}

	if evidence.Kind == EndEvidenceExplicitEnd {
		return canEndAfterLivePositive(state, evidence.EffectiveAt, dbNow, grace)
	}

	if evidence.Kind == EndEvidenceExplicitCancel {
		return canEndExplicitCancel(state, evidence.EffectiveAt, dbNow, grace)
	}

	if evidence.Kind == EndEvidenceScopedAbsence {
		return canEndScopedAbsence(state, evidence, dbNow, grace)
	}

	return false
}

func canEndAfterLivePositive(state *LiveEvidenceClock, effectiveAt, dbNow time.Time, grace time.Duration) bool {
	if state.LastLivePositiveAt == nil || state.LastLivePositiveSeenAt == nil {
		return false
	}

	return effectiveAt.After(*state.LastLivePositiveAt) && !dbNow.Before(state.LastLivePositiveSeenAt.Add(grace))
}

func canEndExplicitCancel(state *LiveEvidenceClock, effectiveAt, dbNow time.Time, grace time.Duration) bool {
	if state.LastLivePositiveAt != nil || state.LastUpcomingPositiveAt == nil || state.LastUpcomingPositiveSeenAt == nil {
		return false
	}

	return effectiveAt.After(*state.LastUpcomingPositiveAt) && !dbNow.Before(state.LastUpcomingPositiveSeenAt.Add(grace))
}

func canEndScopedAbsence(state *LiveEvidenceClock, evidence *EndEvidence, dbNow time.Time, grace time.Duration) bool {
	if !evidence.NegativeEligible || !evidence.ScopeCoversSession {
		return false
	}

	return canEndAfterLivePositive(state, evidence.EffectiveAt, dbNow, grace) && state.ConsecutiveAbsenceSlots >= 2
}
