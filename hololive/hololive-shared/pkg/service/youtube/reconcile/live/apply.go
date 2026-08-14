package live

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func applyUpcomingPositive(session *reduceSession, fact SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if ok && existing.Status == StatusEnded {
		session.applications = append(session.applications, Application{
			EntityKind: "youtube_live_session", EntityKey: fact.VideoID, Decision: "KEEP_ENDED",
		})
		return
	}
	if !ok {
		created := newSession(fact, StatusUpcoming, session.evidence)
		created.Clock.LastUpcomingPositiveAt = copyTime(session.evidence.EffectiveAt)
		created.Clock.LastUpcomingPositiveSeenAt = copyTime(session.evidence.ReceivedAt)
		session.state.Sessions[fact.VideoID] = created
		markDirty(session, fact.VideoID)
		session.applications = append(session.applications, Application{
			EntityKind: "youtube_live_session", EntityKey: fact.VideoID, Decision: "APPLIED",
		})
		reapplyStoredEnds(session, fact.VideoID)
		return
	}
	if existing.Clock.LastUpcomingPositiveAt != nil && session.evidence.EffectiveAt.Before(*existing.Clock.LastUpcomingPositiveAt) {
		session.applications = append(session.applications, Application{
			EntityKind: "youtube_live_session", EntityKey: fact.VideoID, Decision: "OLDER_POSITIVE_RETAINED",
		})
		return
	}
	existing = mergePositiveFields(existing, fact, session.evidence)
	existing.Clock.LastUpcomingPositiveAt = copyTime(session.evidence.EffectiveAt)
	existing.Clock.LastUpcomingPositiveSeenAt = copyTime(session.evidence.ReceivedAt)
	if shouldClearEnd(existing, session.evidence.EffectiveAt) {
		clearEndCandidate(&existing)
		delete(session.state.PendingEnds, fact.VideoID)
	}
	session.state.Sessions[fact.VideoID] = existing
	markDirty(session, fact.VideoID)
	session.applications = append(session.applications, Application{
		EntityKind: "youtube_live_session", EntityKey: fact.VideoID, Decision: "APPLIED",
	})
}

func applyLivePositive(session *reduceSession, fact SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if ok && existing.Status == StatusEnded {
		session.applications = append(session.applications, Application{
			EntityKind: "youtube_live_session", EntityKey: fact.VideoID, Decision: "KEEP_ENDED",
		})
		return
	}
	if !ok {
		created := newSession(fact, StatusLive, session.evidence)
		created.Clock.LastLivePositiveAt = copyTime(session.evidence.EffectiveAt)
		created.Clock.LastLivePositiveSeenAt = copyTime(session.evidence.ReceivedAt)
		created.LiveFirstSeenAt = copyTime(session.evidence.ReceivedAt)
		created.StartedAt = firstTime(fact.StartedAt, session.evidence.EffectiveAt)
		session.state.Sessions[fact.VideoID] = created
		markDirty(session, fact.VideoID)
		session.applications = append(session.applications, Application{
			EntityKind: "youtube_live_session", EntityKey: fact.VideoID, Decision: "APPLIED",
		})
		reapplyStoredEnds(session, fact.VideoID)
		return
	}
	if existing.Clock.LastLivePositiveAt != nil && session.evidence.EffectiveAt.Before(*existing.Clock.LastLivePositiveAt) {
		session.applications = append(session.applications, Application{
			EntityKind: "youtube_live_session", EntityKey: fact.VideoID, Decision: "OLDER_POSITIVE_RETAINED",
		})
		return
	}
	existing = mergePositiveFields(existing, fact, session.evidence)
	if existing.Status == StatusUpcoming {
		existing.Status = StatusLive
	}
	if existing.LiveFirstSeenAt == nil {
		existing.LiveFirstSeenAt = copyTime(session.evidence.ReceivedAt)
	}
	if existing.StartedAt == nil {
		existing.StartedAt = firstTime(fact.StartedAt, session.evidence.EffectiveAt)
	}
	existing.Clock.LastLivePositiveAt = copyTime(session.evidence.EffectiveAt)
	existing.Clock.LastLivePositiveSeenAt = copyTime(session.evidence.ReceivedAt)
	if shouldClearEnd(existing, session.evidence.EffectiveAt) {
		clearEndCandidate(&existing)
		delete(session.state.PendingEnds, fact.VideoID)
	}
	session.state.Sessions[fact.VideoID] = existing
	markDirty(session, fact.VideoID)
	session.applications = append(session.applications, Application{
		EntityKind: "youtube_live_session", EntityKey: fact.VideoID, Decision: "APPLIED",
	})
}

func newSession(fact SessionFact, status Status, evidence Evidence) SessionState {
	return SessionState{
		VideoID:            fact.VideoID,
		ChannelID:          fact.ChannelID,
		Status:             status,
		ScheduledStartTime: copyOptionalTime(fact.ScheduledAt),
		LastSeenAt:         evidence.ReceivedAt.UTC(),
		Present:            true,
	}
}

func mergePositiveFields(existing SessionState, fact SessionFact, evidence Evidence) SessionState {
	if fact.ChannelID != "" {
		existing.ChannelID = fact.ChannelID
	}
	if fact.ScheduledAt != nil {
		existing.ScheduledStartTime = copyOptionalTime(fact.ScheduledAt)
	}
	if fact.StartedAt != nil && existing.StartedAt == nil {
		existing.StartedAt = copyOptionalTime(fact.StartedAt)
	}
	if evidence.ReceivedAt.After(existing.LastSeenAt) {
		existing.LastSeenAt = evidence.ReceivedAt.UTC()
	}
	existing.Present = true
	return existing
}

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
	if !covers {
		return
	}
	if ignoredAbsence(existing, slot.ScheduledFor) {
		return
	}
	if existing.Clock.LastLivePositiveAt == nil {
		existing.IgnoredAbsenceScheduledFor = append(existing.IgnoredAbsenceScheduledFor, slot.ScheduledFor)
		session.state.Sessions[existing.VideoID] = existing
		markDirty(session, existing.VideoID)
		return
	}
	if !slot.EffectiveAt.After(*existing.Clock.LastLivePositiveAt) {
		return
	}
	if replayedAbsence(existing, slot) {
		return
	}
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
		session.state.Sessions[existing.VideoID] = existing
		markDirty(session, existing.VideoID)
		delete(session.state.PendingEnds, existing.VideoID)
		session.applications = append(session.applications, Application{
			EntityKind: "youtube_live_session", EntityKey: existing.VideoID, Decision: "ENDED",
		})
		return
	}
	if persistCandidate(existing, evidence) {
		setEndCandidate(&existing, pending, session.grace)
		session.state.Sessions[existing.VideoID] = existing
		markDirty(session, existing.VideoID)
		session.applications = append(session.applications, Application{
			EntityKind: "youtube_live_session", EntityKey: existing.VideoID, Decision: "END_CANDIDATE",
		})
		return
	}
	if existing.Clock.EndCandidateKind != nil {
		clearEndCandidate(&existing)
		session.state.Sessions[existing.VideoID] = existing
		markDirty(session, existing.VideoID)
		session.applications = append(session.applications, Application{
			EntityKind: "youtube_live_session", EntityKey: existing.VideoID, Decision: "END_CANDIDATE_CLEARED",
		})
	}
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
	switch evidence.Kind {
	case EndEvidenceExplicitEnd:
		return existing.Clock.LastLivePositiveAt != nil && existing.Clock.LastLivePositiveSeenAt != nil &&
			evidence.EffectiveAt.After(*existing.Clock.LastLivePositiveAt)
	case EndEvidenceExplicitCancel:
		return existing.Clock.LastLivePositiveAt == nil &&
			existing.Clock.LastUpcomingPositiveAt != nil &&
			existing.Clock.LastUpcomingPositiveSeenAt != nil &&
			evidence.EffectiveAt.After(*existing.Clock.LastUpcomingPositiveAt)
	case EndEvidenceScopedAbsence:
		return evidence.NegativeEligible && evidence.ScopeCoversSession &&
			existing.Clock.LastLivePositiveAt != nil &&
			existing.Clock.LastLivePositiveSeenAt != nil &&
			evidence.EffectiveAt.After(*existing.Clock.LastLivePositiveAt) &&
			existing.Clock.ConsecutiveAbsenceSlots >= 2
	default:
		return false
	}
}

func settleDueCandidate(session *reduceSession, videoID string) {
	existing, ok := session.state.Sessions[videoID]
	if !ok || existing.Clock.NextEndCheckAt == nil || existing.Clock.NextEndCheckAt.After(session.dbNow) {
		return
	}
	if existing.Status == StatusEnded {
		clearEndCandidate(&existing)
		session.state.Sessions[videoID] = existing
		markDirty(session, videoID)
		return
	}
	pending, ok := session.state.PendingEnds[videoID]
	if !ok {
		clearEndCandidate(&existing)
		session.state.Sessions[videoID] = existing
		markDirty(session, videoID)
		return
	}
	evidence := endEvidenceOf(existing, pending)
	if CanEnd(existing.Clock, evidence, session.dbNow, session.grace) {
		return
	}
	if persistCandidate(existing, evidence) {
		next := candidateRecheckAt(existing, pending, session.grace)
		if next != nil && next.After(session.dbNow) {
			existing.Clock.NextEndCheckAt = next
		} else {
			clearEndCandidate(&existing)
		}
		session.state.Sessions[videoID] = existing
		markDirty(session, videoID)
		return
	}
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
