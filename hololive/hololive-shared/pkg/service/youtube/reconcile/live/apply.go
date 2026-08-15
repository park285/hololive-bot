package live

import (
	"time"
)

func reapplyStoredEnds(session *reduceSession, videoID string) {
	existing, ok := session.state.Sessions[videoID]
	if !ok || existing.Status == StatusEnded {
		return
	}
	pending, ok := session.state.PendingEnds[videoID]
	if !ok {
		return
	}
	applyPendingEnd(session, existing, pending)
}

func applyPendingEnd(session *reduceSession, existing SessionState, pending PendingEnd) {
	evidence := endEvidenceOf(existing, pending)
	if CanEnd(existing.Clock, evidence, session.dbNow, session.grace) {
		endSession(&existing, pending, session.dbNow)
		storeSessionDecision(session, existing, "ENDED")
		delete(session.state.PendingEnds, existing.VideoID)
		return
	}
	if persistCandidate(existing, evidence) {
		setEndCandidate(&existing, pending, session.grace)
		storeSessionDecision(session, existing, "END_CANDIDATE")
		return
	}
	if existing.Clock.EndCandidateKind != nil {
		clearEndCandidate(&existing)
		storeSessionDecision(session, existing, "END_CANDIDATE_CLEARED")
	}
}

func storeSessionDecision(session *reduceSession, existing SessionState, decision string) {
	session.state.Sessions[existing.VideoID] = existing
	markDirty(session, existing.VideoID)
	recordApplication(session, existing.VideoID, decision)
}

func endEvidenceOf(existing SessionState, pending PendingEnd) EndEvidence {
	return EndEvidence{
		Kind:                 pending.Kind,
		EffectiveAt:          pending.EffectiveAt,
		Valid:                true,
		EntityMatchesSession: pending.VideoID == existing.VideoID,
		NegativeEligible:     pending.NegativeEligible,
		ScopeCoversSession:   pending.ScopeCovers,
		HasPositiveAtOrAfter: hasPositiveAtOrAfter(existing, pending),
	}
}

func hasPositiveAtOrAfter(existing SessionState, pending PendingEnd) bool {
	if existing.Clock.LastLivePositiveAt != nil && !existing.Clock.LastLivePositiveAt.Before(pending.EffectiveAt) {
		return true
	}
	if pending.Kind == EndEvidenceExplicitCancel &&
		existing.Clock.LastUpcomingPositiveAt != nil &&
		!existing.Clock.LastUpcomingPositiveAt.Before(pending.EffectiveAt) {
		return true
	}
	return false
}

func persistCandidate(existing SessionState, evidence EndEvidence) bool {
	if !evidence.Valid || !evidence.EntityMatchesSession || evidence.HasPositiveAtOrAfter {
		return false
	}
	return persistCandidateKind(existing, evidence)
}

func persistCandidateKind(existing SessionState, evidence EndEvidence) bool {
	if evidence.Kind == EndEvidenceExplicitEnd {
		return persistExplicitEnd(existing, evidence)
	}
	if evidence.Kind == EndEvidenceExplicitCancel {
		return persistExplicitCancel(existing, evidence)
	}
	if evidence.Kind == EndEvidenceScopedAbsence {
		return persistScopedAbsence(existing, evidence)
	}
	return false
}

func persistExplicitEnd(existing SessionState, evidence EndEvidence) bool {
	return existing.Clock.LastLivePositiveAt != nil && existing.Clock.LastLivePositiveSeenAt != nil &&
		evidence.EffectiveAt.After(*existing.Clock.LastLivePositiveAt)
}

func persistExplicitCancel(existing SessionState, evidence EndEvidence) bool {
	return existing.Clock.LastLivePositiveAt == nil &&
		existing.Clock.LastUpcomingPositiveAt != nil &&
		existing.Clock.LastUpcomingPositiveSeenAt != nil &&
		evidence.EffectiveAt.After(*existing.Clock.LastUpcomingPositiveAt)
}

func persistScopedAbsence(existing SessionState, evidence EndEvidence) bool {
	return evidence.NegativeEligible && evidence.ScopeCoversSession &&
		existing.Clock.LastLivePositiveAt != nil &&
		existing.Clock.LastLivePositiveSeenAt != nil &&
		evidence.EffectiveAt.After(*existing.Clock.LastLivePositiveAt) &&
		existing.Clock.ConsecutiveAbsenceSlots >= 2
}

func settleDueCandidate(session *reduceSession, videoID string) {
	existing, ok := session.state.Sessions[videoID]
	if !ok || existing.Clock.NextEndCheckAt == nil || existing.Clock.NextEndCheckAt.After(session.dbNow) {
		return
	}
	if existing.Status == StatusEnded {
		clearStoredCandidate(session, videoID, existing)
		return
	}
	pending, ok := session.state.PendingEnds[videoID]
	if !ok {
		clearStoredCandidate(session, videoID, existing)
		return
	}
	refreshDueCandidate(session, existing, pending)
}

func refreshDueCandidate(session *reduceSession, existing SessionState, pending PendingEnd) {
	evidence := endEvidenceOf(existing, pending)
	if CanEnd(existing.Clock, evidence, session.dbNow, session.grace) {
		return
	}
	if persistCandidate(existing, evidence) {
		storeRecheckOrClear(session, existing, pending)
		return
	}
	clearStoredCandidate(session, existing.VideoID, existing)
}

func storeRecheckOrClear(session *reduceSession, existing SessionState, pending PendingEnd) {
	next := candidateRecheckAt(existing, pending, session.grace)
	if next != nil && next.After(session.dbNow) {
		existing.Clock.NextEndCheckAt = next
	} else {
		clearEndCandidate(&existing)
	}
	session.state.Sessions[existing.VideoID] = existing
	markDirty(session, existing.VideoID)
}

func clearStoredCandidate(session *reduceSession, videoID string, existing SessionState) {
	clearEndCandidate(&existing)
	session.state.Sessions[videoID] = existing
	markDirty(session, videoID)
}

func candidateRecheckAt(existing SessionState, pending PendingEnd, grace time.Duration) *time.Time {
	seen := existing.Clock.LastLivePositiveSeenAt
	if pending.Kind == EndEvidenceExplicitCancel {
		seen = existing.Clock.LastUpcomingPositiveSeenAt
	}
	if seen == nil {
		return nil
	}
	next := seen.Add(grace)
	return &next
}

func setEndCandidate(existing *SessionState, pending PendingEnd, grace time.Duration) {
	kind := pending.Kind
	observationID := pending.ObservationID
	existing.Clock.EndCandidateKind = &kind
	existing.Clock.EndCandidateObservationID = &observationID
	existing.Clock.LastEndEvidenceAt = copyTime(pending.EffectiveAt)
	existing.Clock.NextEndCheckAt = candidateRecheckAt(*existing, pending, grace)
}

func endSession(existing *SessionState, pending PendingEnd, dbNow time.Time) {
	existing.Status = StatusEnded
	if pending.EndedAt != nil {
		existing.EndedAt = copyOptionalTime(pending.EndedAt)
	} else {
		existing.EndedAt = copyTime(pending.EffectiveAt)
	}
	reason := endReasonOf(pending.Kind)
	existing.EndReason = &reason
	existing.Clock.EndedAt = existing.EndedAt
	existing.Clock.LastEndEvidenceAt = copyTime(pending.EffectiveAt)
	existing.Clock.EndCandidateKind = nil
	existing.Clock.EndCandidateObservationID = nil
	existing.Clock.NextEndCheckAt = nil
	if dbNow.After(existing.LastSeenAt) {
		existing.LastSeenAt = dbNow
	}
}

func endReasonOf(kind EndEvidenceKind) EndReason {
	if kind == EndEvidenceExplicitCancel {
		return EndReasonCancelledBeforeLive
	}
	if kind == EndEvidenceScopedAbsence {
		return EndReasonScopedAbsence
	}
	return EndReasonExplicitEnd
}

func shouldClearEnd(existing SessionState, positiveAt time.Time) bool {
	cutoff := existing.Clock.LastEndEvidenceAt
	if existing.Clock.LastCompleteAbsenceAt != nil {
		if cutoff == nil || existing.Clock.LastCompleteAbsenceAt.After(*cutoff) {
			cutoff = existing.Clock.LastCompleteAbsenceAt
		}
	}
	if cutoff == nil {
		return existing.Clock.EndCandidateKind != nil
	}
	return !positiveAt.Before(*cutoff)
}

func clearEndCandidate(existing *SessionState) {
	existing.Clock.EndCandidateKind = nil
	existing.Clock.EndCandidateObservationID = nil
	existing.Clock.NextEndCheckAt = nil
	existing.Clock.LastEndEvidenceAt = nil
	existing.Clock.LastCompleteAbsenceAt = nil
	existing.Clock.ConsecutiveAbsenceSlots = 0
	existing.FirstAbsenceScheduledFor = nil
	existing.SecondAbsenceScheduledFor = nil
	existing.LastAbsenceObservationID = 0
	existing.LastAbsenceScheduledFor = nil
}

func latestPositiveAt(existing SessionState) time.Time {
	latest := time.Time{}
	if existing.Clock.LastLivePositiveAt != nil {
		latest = *existing.Clock.LastLivePositiveAt
	}
	if existing.Clock.LastUpcomingPositiveAt != nil && existing.Clock.LastUpcomingPositiveAt.After(latest) {
		latest = *existing.Clock.LastUpcomingPositiveAt
	}
	return latest
}

func firstTime(value *time.Time, fallback time.Time) *time.Time {
	if value != nil {
		return copyOptionalTime(value)
	}
	return copyTime(fallback)
}

func sameOptionalTime(value *time.Time, other *time.Time) bool {
	if value == nil || other == nil {
		return false
	}
	return value.Equal(*other)
}
