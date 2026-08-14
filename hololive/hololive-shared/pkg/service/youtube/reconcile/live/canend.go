package live

import "time"

func CanEnd(state LiveEvidenceClock, evidence EndEvidence, dbNow time.Time, grace time.Duration) bool {
	if !evidence.Valid || !evidence.EntityMatchesSession || evidence.HasPositiveAtOrAfter {
		return false
	}

	switch evidence.Kind {
	case EndEvidenceExplicitEnd:
		if state.LastLivePositiveAt == nil || state.LastLivePositiveSeenAt == nil {
			return false
		}
		return evidence.EffectiveAt.After(*state.LastLivePositiveAt) &&
			!dbNow.Before(state.LastLivePositiveSeenAt.Add(grace))

	case EndEvidenceExplicitCancel:
		if state.LastLivePositiveAt != nil ||
			state.LastUpcomingPositiveAt == nil ||
			state.LastUpcomingPositiveSeenAt == nil {
			return false
		}
		return evidence.EffectiveAt.After(*state.LastUpcomingPositiveAt) &&
			!dbNow.Before(state.LastUpcomingPositiveSeenAt.Add(grace))

	case EndEvidenceScopedAbsence:
		if !evidence.NegativeEligible ||
			!evidence.ScopeCoversSession ||
			state.LastLivePositiveAt == nil ||
			state.LastLivePositiveSeenAt == nil {
			return false
		}
		return evidence.EffectiveAt.After(*state.LastLivePositiveAt) &&
			!dbNow.Before(state.LastLivePositiveSeenAt.Add(grace)) &&
			state.ConsecutiveAbsenceSlots >= 2

	default:
		return false
	}
}
