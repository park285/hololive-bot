package live

import "github.com/kapu/hololive-shared/pkg/domain"

func applyUpcomingPositive(session *reduceSession, fact *SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if ok && existing.Status == domain.LiveStatusEnded {
		recordApplication(session, fact.VideoID, "KEEP_ENDED")

		return
	}

	if !ok {
		created := newSession(fact, domain.LiveStatusUpcoming, session.evidence)

		created.Clock.LastUpcomingPositiveAt = copyTime(session.evidence.EffectiveAt)
		created.Clock.LastUpcomingPositiveSeenAt = copyTime(session.evidence.ReceivedAt)
		storeAppliedSession(session, fact.VideoID, &created)
		reapplyStoredEnds(session, fact.VideoID)

		return
	}

	if existing.Status == domain.LiveStatusLive && existing.Clock.LastLivePositiveAt == nil {
		// UPCOMING 사실만으로 근거 없는 기존 LIVE에 LIVE head를 만들어 면제하지 않는다.
		recordApplication(session, fact.VideoID, "LIVE_WITHOUT_POSITIVE_RETAINED")

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
	if ok && existing.Status == domain.LiveStatusEnded {
		recordApplication(session, fact.VideoID, "KEEP_ENDED")

		return
	}

	if !fact.LiveStartConfirmed {
		applyUnconfirmedLive(session, fact, &existing, ok)

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

func applyUnconfirmedLive(session *reduceSession, fact *SessionFact, existing *SessionState, ok bool) {
	if !ok {
		created := newSession(fact, domain.LiveStatusUpcoming, session.evidence)
		storeUnconfirmedLive(session, fact.VideoID, &created)

		return
	}

	merged := mergePositiveFields(existing, fact, session.evidence)

	merged.StartedAt = copyOptionalTime(existing.StartedAt)
	storeUnconfirmedLive(session, fact.VideoID, &merged)
}

func storeUnconfirmedLive(session *reduceSession, videoID string, state *SessionState) {
	if shouldClearEnd(state, session.evidence.EffectiveAt) {
		clearEndCandidate(state)
		delete(session.state.PendingEnds, videoID)
	}

	session.state.Sessions[videoID] = *state
	markDirty(session, videoID)
	recordApplication(session, videoID, "LIVE_START_UNCONFIRMED")
}

func newLiveSession(fact *SessionFact, evidence *Evidence) SessionState {
	created := newSession(fact, domain.LiveStatusLive, evidence)

	created.Clock.LastLivePositiveAt = copyTime(evidence.EffectiveAt)
	created.Clock.LastLivePositiveSeenAt = copyTime(evidence.ReceivedAt)
	created.LiveFirstSeenAt = copyTime(evidence.ReceivedAt)
	created.StartedAt = firstTime(fact.StartedAt, evidence.EffectiveAt)

	return created
}

func mergeLiveSession(existing *SessionState, fact *SessionFact, evidence *Evidence) SessionState {
	merged := mergePositiveFields(existing, fact, evidence)
	if merged.Status == domain.LiveStatusUpcoming {
		merged.Status = domain.LiveStatusLive
	}

	merged.StatusObservedAt = copyTime(evidence.EffectiveAt)

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

func newSession(fact *SessionFact, status domain.LiveStatus, evidence *Evidence) SessionState {
	// 새 세션에는 무시한 부재가 없으므로 미적재가 아니라 적재된 빈 이력으로 시작한다.
	created := SessionState{
		VideoID:            fact.VideoID,
		ChannelID:          fact.ChannelID,
		Status:             status,
		LifecycleOrigin:    OriginMetadataOnly,
		Title:              fact.Title,
		TopicID:            fact.TopicID,
		ThumbnailURL:       fact.ThumbnailURL,
		ScheduledStartTime: copyOptionalTime(fact.ScheduledAt),
		LastSeenAt:         evidence.ReceivedAt.UTC(),
		IgnoredAbsences:    LoadedIgnoredAbsences(nil),
		Present:            true,
	}
	if fact.Title != "" {
		created.TitleObservedAt = copyTime(evidence.EffectiveAt)
	}

	if fact.Status == string(status) && (status != domain.LiveStatusLive || fact.LiveStartConfirmed) {
		created.StatusObservedAt = copyTime(evidence.EffectiveAt)
	}

	if fact.ScheduledAt != nil {
		created.ScheduleObservedAt = copyTime(evidence.EffectiveAt)
	}

	return created
}

func mergePositiveFields(existing *SessionState, fact *SessionFact, evidence *Evidence) SessionState {
	merged := *existing

	if fact.ChannelID != "" {
		merged.ChannelID = fact.ChannelID
	}

	if fact.Title != "" && (existing.TitleObservedAt == nil || evidence.EffectiveAt.After(*existing.TitleObservedAt)) {
		merged.Title = fact.Title
		merged.TitleObservedAt = copyTime(evidence.EffectiveAt)
	}

	if fact.TopicID != "" {
		merged.TopicID = fact.TopicID
	}

	if fact.ThumbnailURL != "" {
		merged.ThumbnailURL = fact.ThumbnailURL
	}

	if fact.ScheduledAt != nil && (existing.ScheduleObservedAt == nil || evidence.EffectiveAt.After(*existing.ScheduleObservedAt)) {
		merged.ScheduledStartTime = copyOptionalTime(fact.ScheduledAt)
		merged.ScheduleObservedAt = copyTime(evidence.EffectiveAt)
	}

	// 유지된 LIVE/ENDED나 시작 미확정 metadata를 새 상태 관측으로 해석하지 않는다.
	if fact.Status == string(merged.Status) && (merged.Status != domain.LiveStatusLive || fact.LiveStartConfirmed) {
		merged.StatusObservedAt = copyTime(evidence.EffectiveAt)
	}

	if fact.StartedAt != nil && merged.StartedAt == nil {
		merged.StartedAt = copyOptionalTime(fact.StartedAt)
	}

	if evidence.ReceivedAt.After(merged.LastSeenAt) {
		merged.LastSeenAt = evidence.ReceivedAt.UTC()
	}

	// positive는 공급자가 영상을 아직 해석한다는 뜻이므로 해소 불가 추적을 지운다.
	merged.Clock.UnresolvableSince = nil
	merged.Present = true

	return merged
}

func storeAppliedSession(session *reduceSession, videoID string, existing *SessionState) {
	existing.LifecycleOrigin = OriginObserved
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
