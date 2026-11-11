package live

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func anyPositiveAtOrAfter(clock *LiveEvidenceClock, at time.Time) bool {
	for _, positive := range []*time.Time{clock.LastUpcomingPositiveAt, clock.LastLivePositiveAt} {
		if positive != nil && !positive.Before(at) {
			return true
		}
	}

	return false
}

// clearUnresolvableTracking은 추적 시작 이후에 관측된 positive만 해소 불가 추적을 지우게 한다. 그런 positive는
// 공급자가 영상을 아직 해석한다는 뜻이다. 추적 시작보다 이른 관측이 늦게 도착한 경우는 그 뒤의 해석 불가를
// 반박하지 못하므로 추적을 유지한다.
func clearUnresolvableTracking(clock *LiveEvidenceClock, observedAt time.Time) {
	if clock.UnresolvableSince != nil && !observedAt.Before(*clock.UnresolvableSince) {
		clock.UnresolvableSince = nil
	}
}

// UnresolvableTrackable은 해소 불가 추적을 저장할 수 있는 세션인지 판정한다. 시작을 관측한 LIVE와, head가
// 있거나 관측 출처인 UPCOMING만 대상이다. 그 밖의 head 없는 metadata_only·legacy_unknown UPCOMING은 미확정
// 메타데이터가 authoritative head의 근거가 아니므로 head를 만들지 않으며, 추적도 기록하지 않는다(기존 결정 코드 유지).
func UnresolvableTrackable(existing *SessionState) bool {
	switch existing.Status {
	case domain.LiveStatusLive:
		return existing.Clock.LastLivePositiveAt != nil && existing.Clock.LastLivePositiveSeenAt != nil
	case domain.LiveStatusUpcoming:
		return existing.HeadPresent || existing.LifecycleOrigin == OriginObserved
	case domain.LiveStatusEnded:
		return false
	default:
		return false
	}
}

// 첫 관측 시각을 head에 두고, 시작을 관측한 LIVE는 consumer가 지속 시간과 같은 채널의 /live 음성까지 검증한 사실(VerifiedTerminal)만 마지막 positive의 grace가 지난
// 뒤 UNRESOLVABLE_VIDEO로 끝낸다. 종료 시각은 실제 종료의 하한인 첫 identity_missing 시각이며 시작·알림은 만들지 않는다. UPCOMING은 추적만 한다. 비공개 예약이 공개로
// 돌아오면 시작 알림이 필요하고 시작 감지는 채널 스냅샷 positive가 맡으므로, 추적 기간은 projection이 영상 확인 재확인 주기를 늦추는 근거로만 쓴다.
func applyUnresolvableVideo(session *reduceSession, fact *SessionFact) {
	existing, ok := session.state.Sessions[fact.VideoID]
	if !ok || !existing.Present || fact.ChannelID != existing.ChannelID || !UnresolvableTrackable(&existing) {
		recordApplication(session, fact.VideoID, "UNRESOLVABLE_IGNORED")

		return
	}

	if anyPositiveAtOrAfter(&existing.Clock, session.evidence.EffectiveAt) {
		recordApplication(session, fact.VideoID, "NEWER_POSITIVE_RETAINED")

		return
	}

	since := existing.Clock.UnresolvableSince
	if since == nil {
		existing.Clock.UnresolvableSince = copyTime(session.evidence.EffectiveAt)
		storeSessionDecision(session, &existing, "UNRESOLVABLE_TRACKED")

		return
	}

	if existing.Status != domain.LiveStatusLive || !fact.VerifiedTerminal ||
		session.dbNow.Before(existing.Clock.LastLivePositiveSeenAt.Add(session.grace)) {
		recordApplication(session, fact.VideoID, "UNRESOLVABLE_RETAINED")

		return
	}

	terminateSession(&existing, *since, session.evidence.EffectiveAt, EndReasonUnresolvableVideo, session.dbNow)
	storeSessionDecision(session, &existing, "ENDED")
	delete(session.state.PendingEnds, fact.VideoID)
}
