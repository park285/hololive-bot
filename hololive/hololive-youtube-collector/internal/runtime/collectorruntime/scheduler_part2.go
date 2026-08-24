package collectorruntime

import (
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func (s *leaseScheduler) reportFatal(err error) {
	if s == nil || err == nil {
		return
	}

	s.fatalOnce.Do(func() {
		s.emitFatal(collecterr.Normalize(err))
	})
}

func (s *leaseScheduler) emitFatal(err error) {
	s.cancelDiscoveryAndWorkers()

	if s.fatal == nil {
		return
	}

	select {
	case s.fatal <- err:
	default:
	}
}

func (s *leaseScheduler) cancelDiscoveryAndWorkers() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lifecycleState() == SchedulerRunning {
		s.state = SchedulerStopping
	}

	if s.cancel != nil {
		s.cancel()
	}
}

func (s *leaseScheduler) Snapshot() SchedulerSnapshot {
	if s == nil {
		return SchedulerSnapshot{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	depth := 0
	oldestQueueAge := time.Duration(0)

	if s.queue != nil {
		depth = len(s.queue)
	}

	now := time.Now()

	for _, queuedAt := range s.queuedAt {
		age := now.Sub(queuedAt)
		if age > oldestQueueAge {
			oldestQueueAge = age
		}
	}

	return SchedulerSnapshot{
		State:                  s.lifecycleState(),
		QueueDepth:             depth,
		QueueCapacity:          s.config.QueueCapacity,
		Discovered:             s.discovered,
		Enqueued:               s.enqueued,
		Deduped:                s.deduped,
		QueueFull:              s.queueFull,
		DiscoveryTruncated:     s.discoveryTruncated,
		Projection:             s.projection,
		RotationCursor:         s.rotationCursor,
		CycleStartedAt:         s.cycleStartedAt,
		LastCycleCompletedAt:   s.lastCycleCompletedAt,
		LastCycleOperationCode: s.lastCycleOperationCode,
		OldestQueueAge:         oldestQueueAge,
	}
}

func (s *leaseScheduler) lifecycleState() SchedulerState {
	if s.state == "" {
		return SchedulerNew
	}

	return s.state
}
