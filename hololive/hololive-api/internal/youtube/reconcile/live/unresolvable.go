package live

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// anyPositiveAtOrAfter는 관측 시각 이후의 positive가 있어 종료 근거가 더 오래된 사실인지 판정한다.
func anyPositiveAtOrAfter(clock *LiveEvidenceClock, at time.Time) bool {
	for _, positive := range []*time.Time{clock.LastUpcomingPositiveAt, clock.LastLivePositiveAt} {
		if positive != nil && !positive.Before(at) {
			return true
		}
	}

	return false
}

// applyUnresolvableVideo는 익명 수집이 identity를 확인할 수 없게 된 영상의 추적과 종료를 정산한다.
// 첫 관측 시각을 head에 두고, 시작을 관측한 LIVE는 consumer가 지속 시간과 같은 채널의 /live 음성까지
// 검증한 사실(VerifiedTerminal)만 마지막 positive의 grace가 지난 뒤 UNRESOLVABLE_VIDEO로 끝낸다.
// 종료 시각은 실제 종료의 하한인 첫 identity_missing 시각이며 시작·알림은 만들지 않는다.
// UPCOMING은 추적만 한다. 비공개 예약이 공개로 돌아오면 시작 알림이 필요하고 시작 감지는 채널 스냅샷
// positive가 맡으므로, 추적 기간은 projection이 영상 확인 재확인 주기를 늦추는 근거로만 쓴다.
func applyUnresolvableVideo(session *reduceSession, fact *SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if !ok || !existing.Present || fact.ChannelID != existing.ChannelID || !unresolvableTrackable(&existing) {
		recordApplication(session, fact.VideoID, "UNRESOLVABLE_IGNORED")

		return
	}

	if anyPositiveAtOrAfter(&existing.Clock, session.evidence.EffectiveAt) {
		recordApplication(session, fact.VideoID, "NEWER_POSITIVE_RETAINED")

		return
	}

	if existing.Clock.UnresolvableSince == nil {
		existing.Clock.UnresolvableSince = copyTime(session.evidence.EffectiveAt)
		storeSessionDecision(session, &existing, "UNRESOLVABLE_TRACKED")

		return
	}

	if existing.Status != domain.LiveStatusLive || !fact.VerifiedTerminal ||
		session.dbNow.Before(existing.Clock.LastLivePositiveSeenAt.Add(session.grace)) {
		recordApplication(session, fact.VideoID, "UNRESOLVABLE_RETAINED")

		return
	}

	terminateSession(&existing, *existing.Clock.UnresolvableSince, session.evidence.EffectiveAt, EndReasonUnresolvableVideo, session.dbNow)
	storeSessionDecision(session, &existing, "ENDED")
	delete(session.state.PendingEnds, fact.VideoID)
}

// unresolvableTrackable은 시작을 관측한 LIVE와 UPCOMING만 해소 불가 추적 대상으로 삼는다.
func unresolvableTrackable(existing *SessionState) bool {
	switch existing.Status {
	case domain.LiveStatusLive:
		return existing.Clock.LastLivePositiveAt != nil && existing.Clock.LastLivePositiveSeenAt != nil
	case domain.LiveStatusUpcoming:
		return true
	case domain.LiveStatusEnded:
		return false
	default:
		return false
	}
}
