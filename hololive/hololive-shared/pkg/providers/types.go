package providers

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

// YouTubeStack - YouTube 관련 서비스 묶음 (선택적 활성화)
type YouTubeStack struct {
	Service   youtube.Service
	Scheduler youtube.Scheduler
	StatsRepo *youtube.StatsRepository
}

// PollerIntervals - 폴러별 실행 간격 설정
type PollerIntervals struct {
	Videos    time.Duration // 영상 감지 (기본: 10분)
	Shorts    time.Duration // 쇼츠 감지 (기본: 15분)
	Community time.Duration // 커뮤니티 포스트 (기본: 30분)
	Stats     time.Duration // 채널 통계 (기본: 6시간)
	Live      time.Duration // 라이브 상태 (기본: 3분)
}

// DefaultPollerIntervals - 기본 폴러 실행 간격
func DefaultPollerIntervals() PollerIntervals {
	return PollerIntervals{
		Videos:    10 * time.Minute,
		Shorts:    15 * time.Minute,
		Community: 30 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      3 * time.Minute,
	}
}
