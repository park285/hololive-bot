package scheduleposter

import (
	"context"
	"time"
)

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
