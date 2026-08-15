package live

func applyUpcomingPositive(session *reduceSession, fact SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if ok && existing.Status == StatusEnded {
		recordApplication(session, fact.VideoID, "KEEP_ENDED")
		return
	}
	if !ok {
		created := newSession(fact, StatusUpcoming, session.evidence)
		created.Clock.LastUpcomingPositiveAt = copyTime(session.evidence.EffectiveAt)
		created.Clock.LastUpcomingPositiveSeenAt = copyTime(session.evidence.ReceivedAt)
		storeAppliedSession(session, fact.VideoID, created)
		reapplyStoredEnds(session, fact.VideoID)
		return
	}
	if existing.Clock.LastUpcomingPositiveAt != nil && session.evidence.EffectiveAt.Before(*existing.Clock.LastUpcomingPositiveAt) {
		recordApplication(session, fact.VideoID, "OLDER_POSITIVE_RETAINED")
		return
	}
	existing = mergePositiveFields(existing, fact, session.evidence)
	existing.Clock.LastUpcomingPositiveAt = copyTime(session.evidence.EffectiveAt)
	existing.Clock.LastUpcomingPositiveSeenAt = copyTime(session.evidence.ReceivedAt)
	storePositiveAfterMerge(session, fact.VideoID, existing)
}

func applyLivePositive(session *reduceSession, fact SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if ok && existing.Status == StatusEnded {
		recordApplication(session, fact.VideoID, "KEEP_ENDED")
		return
	}
	if !ok {
		storeAppliedSession(session, fact.VideoID, newLiveSession(fact, session.evidence))
		reapplyStoredEnds(session, fact.VideoID)
		return
	}
	if existing.Clock.LastLivePositiveAt != nil && session.evidence.EffectiveAt.Before(*existing.Clock.LastLivePositiveAt) {
		recordApplication(session, fact.VideoID, "OLDER_POSITIVE_RETAINED")
		return
	}
	storePositiveAfterMerge(session, fact.VideoID, mergeLiveSession(existing, fact, session.evidence))
}

func newLiveSession(fact SessionFact, evidence Evidence) SessionState {
	created := newSession(fact, StatusLive, evidence)
	created.Clock.LastLivePositiveAt = copyTime(evidence.EffectiveAt)
	created.Clock.LastLivePositiveSeenAt = copyTime(evidence.ReceivedAt)
	created.LiveFirstSeenAt = copyTime(evidence.ReceivedAt)
	created.StartedAt = firstTime(fact.StartedAt, evidence.EffectiveAt)
	return created
}

func mergeLiveSession(existing SessionState, fact SessionFact, evidence Evidence) SessionState {
	existing = mergePositiveFields(existing, fact, evidence)
	if existing.Status == StatusUpcoming {
		existing.Status = StatusLive
	}
	if existing.LiveFirstSeenAt == nil {
		existing.LiveFirstSeenAt = copyTime(evidence.ReceivedAt)
	}
	if existing.StartedAt == nil {
		existing.StartedAt = firstTime(fact.StartedAt, evidence.EffectiveAt)
	}
	existing.Clock.LastLivePositiveAt = copyTime(evidence.EffectiveAt)
	existing.Clock.LastLivePositiveSeenAt = copyTime(evidence.ReceivedAt)
	return existing
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

func storeAppliedSession(session *reduceSession, videoID string, existing SessionState) {
	session.state.Sessions[videoID] = existing
	markDirty(session, videoID)
	recordApplication(session, videoID, "APPLIED")
}

func storePositiveAfterMerge(session *reduceSession, videoID string, existing SessionState) {
	if shouldClearEnd(existing, session.evidence.EffectiveAt) {
		clearEndCandidate(&existing)
		delete(session.state.PendingEnds, videoID)
	}
	storeAppliedSession(session, videoID, existing)
}

func recordApplication(session *reduceSession, videoID, decision string) {
	session.applications = append(session.applications, Application{
		EntityKind: "youtube_live_session", EntityKey: videoID, Decision: decision,
	})
}
