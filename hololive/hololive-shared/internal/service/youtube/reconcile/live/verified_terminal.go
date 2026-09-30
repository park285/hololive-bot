package live

import "time"

// 영상 consumer가 identity·관측 시각·현재 사실을 검증한 종료만 시작 미관측을
// 정산한다. 일반 snapshot/finalizer의 positive·grace 계약은 이 경로를 사용하지 않는다.
func applyVerifiedVideoEnd(session *reduceSession, fact *SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if !ok || !existing.Present || existing.Status == StatusEnded ||
		fact.ChannelID != existing.ChannelID || fact.EndedAt == nil || fact.EndedAt.IsZero() {
		return
	}

	for _, positive := range []*time.Time{existing.Clock.LastUpcomingPositiveAt, existing.Clock.LastLivePositiveAt} {
		if positive != nil && (!positive.Before(session.evidence.EffectiveAt) || !positive.Before(*fact.EndedAt)) {
			return
		}
	}

	if pending, exists := session.state.PendingEnds[fact.VideoID]; exists && pending.EffectiveAt.After(session.evidence.EffectiveAt) {
		return
	}

	pending := pendingFromFact(session, fact, EndEvidenceExplicitEnd)
	endSession(&existing, &pending, session.dbNow)
	storeSessionDecision(session, &existing, "ENDED")
	delete(session.state.PendingEnds, fact.VideoID)
}
