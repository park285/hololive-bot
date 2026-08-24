package live

func applyUpcomingPositive(session *reduceSession, fact *SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if ok && existing.Status == StatusEnded {
		recordApplication(session, fact.VideoID, "KEEP_ENDED")

		return
	}

	if !ok {
		created := newSession(fact, StatusUpcoming, session.evidence)

		created.Clock.LastUpcomingPositiveAt = copyTime(session.evidence.EffectiveAt)
		created.Clock.LastUpcomingPositiveSeenAt = copyTime(session.evidence.ReceivedAt)
		storeAppliedSession(session, fact.VideoID, &created)
		reapplyStoredEnds(session, fact.VideoID)

		return
	}

	if existing.Clock.LastUpcomingPositiveAt != nil && session.evidence.EffectiveAt.Before(*existing.Clock.LastUpcomingPositiveAt) {
		recordApplication(session, fact.VideoID, "OLDER_POSITIVE_RETAINED")

		return
	}

	existing = mergePositiveFields(&existing, fact, session.evidence)
	existing.Clock.LastUpcomingPositiveAt = copyTime(session.evidence.EffectiveAt)
	existing.Clock.LastUpcomingPositiveSeenAt = copyTime(session.evidence.ReceivedAt)
	storePositiveAfterMerge(session, fact.VideoID, &existing)
}

func applyLivePositive(session *reduceSession, fact *SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if ok && existing.Status == StatusEnded {
		recordApplication(session, fact.VideoID, "KEEP_ENDED")

		return
	}

	if !ok {
		created := newLiveSession(fact, session.evidence)
		storeAppliedSession(session, fact.VideoID, &created)
		reapplyStoredEnds(session, fact.VideoID)

		return
	}

	if existing.Clock.LastLivePositiveAt != nil && session.evidence.EffectiveAt.Before(*existing.Clock.LastLivePositiveAt) {
		recordApplication(session, fact.VideoID, "OLDER_POSITIVE_RETAINED")

		return
	}

	merged := mergeLiveSession(&existing, fact, session.evidence)
	storePositiveAfterMerge(session, fact.VideoID, &merged)
}

func newLiveSession(fact *SessionFact, evidence *Evidence) SessionState {
	created := newSession(fact, StatusLive, evidence)

	created.Clock.LastLivePositiveAt = copyTime(evidence.EffectiveAt)
	created.Clock.LastLivePositiveSeenAt = copyTime(evidence.ReceivedAt)
	created.LiveFirstSeenAt = copyTime(evidence.ReceivedAt)
	created.StartedAt = firstTime(fact.StartedAt, evidence.EffectiveAt)

	return created
}

func mergeLiveSession(existing *SessionState, fact *SessionFact, evidence *Evidence) SessionState {
	merged := mergePositiveFields(existing, fact, evidence)
	if merged.Status == StatusUpcoming {
		merged.Status = StatusLive
	}

	if merged.LiveFirstSeenAt == nil {
		merged.LiveFirstSeenAt = copyTime(evidence.ReceivedAt)
	}

	if merged.StartedAt == nil {
		merged.StartedAt = firstTime(fact.StartedAt, evidence.EffectiveAt)
	}

	merged.Clock.LastLivePositiveAt = copyTime(evidence.EffectiveAt)
	merged.Clock.LastLivePositiveSeenAt = copyTime(evidence.ReceivedAt)

	return merged
}

func newSession(fact *SessionFact, status Status, evidence *Evidence) SessionState {
	return SessionState{
		VideoID:            fact.VideoID,
		ChannelID:          fact.ChannelID,
		Status:             status,
		ScheduledStartTime: copyOptionalTime(fact.ScheduledAt),
		LastSeenAt:         evidence.ReceivedAt.UTC(),
		Present:            true,
	}
}

func mergePositiveFields(existing *SessionState, fact *SessionFact, evidence *Evidence) SessionState {
	merged := *existing

	if fact.ChannelID != "" {
		merged.ChannelID = fact.ChannelID
	}

	if fact.ScheduledAt != nil {
		merged.ScheduledStartTime = copyOptionalTime(fact.ScheduledAt)
	}

	if fact.StartedAt != nil && merged.StartedAt == nil {
		merged.StartedAt = copyOptionalTime(fact.StartedAt)
	}

	if evidence.ReceivedAt.After(merged.LastSeenAt) {
		merged.LastSeenAt = evidence.ReceivedAt.UTC()
	}

	merged.Present = true

	return merged
}

func storeAppliedSession(session *reduceSession, videoID string, existing *SessionState) {
	session.state.Sessions[videoID] = *existing
	markDirty(session, videoID)
	recordApplication(session, videoID, "APPLIED")
}

func storePositiveAfterMerge(session *reduceSession, videoID string, existing *SessionState) {
	if shouldClearEnd(existing, session.evidence.EffectiveAt) {
		clearEndCandidate(existing)
		delete(session.state.PendingEnds, videoID)
	}

	storeAppliedSession(session, videoID, existing)
}

func recordApplication(session *reduceSession, videoID, decision string) {
	session.applications = append(session.applications, Application{
		EntityKind: "youtube_live_session", EntityKey: videoID, Decision: decision,
	})
}
