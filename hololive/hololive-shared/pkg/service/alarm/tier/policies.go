package tier

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// ComputeNextCheckAt: 방송 시작까지 남은 시간 기반으로 다음 체크 시각을 계산한다.
// nearestStart가 nil이면 예정 없음 로직을 적용한다.
func ComputeNextCheckAt(nearestStart *time.Time, lastNotifiedAt *time.Time) time.Time {
	now := time.Now()

	if nearestStart == nil {
		// 예정 없음: 최근 알림 발송 채널은 고빈도 폴링 유지
		if lastNotifiedAt != nil {
			elapsed := now.Sub(*lastNotifiedAt)
			if elapsed <= constants.RecentlyNotifiedWindow {
				return now.Add(constants.Tier2Interval)
			}
		}
		return now.Add(constants.NoUpcomingInterval)
	}

	timeToStart := nearestStart.Sub(now)

	// 이미 지남 또는 현재 -> Tier1
	if timeToStart <= 0 {
		return now.Add(constants.Tier1Interval)
	}

	switch {
	case timeToStart <= constants.Tier1Window:
		return now.Add(constants.Tier1Interval)
	case timeToStart <= constants.Tier2Window:
		return now.Add(constants.Tier2Interval)
	case timeToStart <= constants.Tier3Window:
		return now.Add(constants.Tier3Interval)
	default:
		return now.Add(constants.Tier4Interval)
	}
}
