package providers

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

// YouTubeStack - YouTube 관련 서비스 묶음 (선택적 활성화)
type YouTubeStack struct {
	Service   youtube.Service
	Scheduler youtube.Scheduler
	StatsRepo *ytstats.StatsRepository
}

// GetService: nil-safe YouTube Service 반환.
func (s *YouTubeStack) GetService() youtube.Service {
	if s == nil {
		return nil
	}
	return s.Service
}

// GetScheduler: nil-safe YouTube Scheduler 반환.
func (s *YouTubeStack) GetScheduler() youtube.Scheduler {
	if s == nil {
		return nil
	}
	return s.Scheduler
}

// GetStatsRepo: nil-safe YouTube StatsRepository 반환.
func (s *YouTubeStack) GetStatsRepo() *ytstats.StatsRepository {
	if s == nil {
		return nil
	}
	return s.StatsRepo
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
