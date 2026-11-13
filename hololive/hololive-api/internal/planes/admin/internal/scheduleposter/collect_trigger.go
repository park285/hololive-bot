package scheduleposter

import (
	"context"
	"time"
)

// RequestCollect는 수집 주기를 기다리지 않고 수집을 한 번 더 돌리게 하며, 이미 요청이 대기 중이면 false다.
// 수집은 Start의 수집 goroutine만 돌리므로 요청이 겹쳐도 두 수집이 동시에 돌지 않는다.
func (s *Service) RequestCollect() bool {
	select {
	case s.collectNow <- struct{}{}:
		s.logger.Info("Schedule poster collection requested")

		return true
	default:
		return false
	}
}

func (s *Service) runCollectLoop(ctx context.Context) {
	timer := time.NewTimer(initialCollectWait)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-s.collectNow:
		}

		if ctx.Err() != nil {
			return
		}

		s.runTask(ctx, "collect", s.collect)
		timer.Reset(s.config.PollInterval())
	}
}
