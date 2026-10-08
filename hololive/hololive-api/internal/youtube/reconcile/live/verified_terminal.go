package live

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// TerminalEndBlockedByPositive는 시작 미관측 종료가 positive 때문에 아직 끝날 수 없는지 판정한다.
// 수집 예정 시각을 EffectiveAt으로 쓰는 provider positive는 실제 ended_at보다 늦을 수 있으므로, 종료
// 시각 이후의 positive는 grace 안에서 관측된 동안만 종료를 막는다. 더 새로운 positive 없이 grace가
// 지나면 공급자 지연으로 보고 검증된 명시적 종료를 받아들인다. 관측 시각이 없는 positive는 신선도를
// 증명할 수 없으므로 계속 막는다.
func TerminalEndBlockedByPositive(clock *LiveEvidenceClock, endedAt, dbNow time.Time, grace time.Duration) bool {
	positives := [...]struct{ at, seen *time.Time }{
		{clock.LastUpcomingPositiveAt, clock.LastUpcomingPositiveSeenAt},
		{clock.LastLivePositiveAt, clock.LastLivePositiveSeenAt},
	}

	for _, positive := range positives {
		if positive.at == nil || positive.at.Before(endedAt) {
			continue
		}

		if positive.seen == nil || dbNow.Before(positive.seen.Add(grace)) {
			return true
		}
	}

	return false
}

// 영상 consumer가 identity·관측 시각·현재 사실을 검증한 종료만 시작 미관측을
// 정산한다. 일반 snapshot/finalizer의 positive·grace 계약은 이 경로를 사용하지 않는다.
func applyVerifiedVideoEnd(session *reduceSession, fact *SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if !ok || !existing.Present || existing.Status == domain.LiveStatusEnded ||
		fact.ChannelID != existing.ChannelID || fact.EndedAt == nil || fact.EndedAt.IsZero() {
		return
	}

	for _, positive := range []*time.Time{existing.Clock.LastUpcomingPositiveAt, existing.Clock.LastLivePositiveAt} {
		if positive != nil && !positive.Before(session.evidence.EffectiveAt) {
			return
		}
	}

	if TerminalEndBlockedByPositive(&existing.Clock, *fact.EndedAt, session.dbNow, session.grace) {
		return
	}

	if pending, exists := session.state.PendingEnds[fact.VideoID]; exists && pending.EffectiveAt.After(session.evidence.EffectiveAt) {
		return
	}

	pending := pendingFromFact(session, fact, EndEvidenceExplicitEnd)
	endSession(&existing, &pending, session.dbNow)
	storeSessionDecision(session, &existing, "ENDED")
	delete(session.state.PendingEnds, fact.VideoID)
}
