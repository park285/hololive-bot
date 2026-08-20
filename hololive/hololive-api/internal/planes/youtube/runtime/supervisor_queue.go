package runtime

import (
	"context"
	"sync/atomic"
	"time"
)

const queueObservationMinInterval = 5 * time.Second

type queueObservationThrottle struct {
	last atomic.Int64
}

func (t *queueObservationThrottle) acquire(now time.Time) bool {
	last := t.last.Load()
	if elapsed := now.Sub(time.Unix(0, last)); elapsed >= 0 && elapsed < queueObservationMinInterval {
		return false
	}
	return t.last.CompareAndSwap(last, now.UnixNano())
}

var queueObservation queueObservationThrottle

func (r *Runtime) observePendingQueue(ctx context.Context) {
	if r == nil || r.pool == nil {
		return
	}
	if cap(r.workCh) > 0 {
		youtubeWorkQueueUtilization.Set(float64(len(r.workCh)) / float64(cap(r.workCh)))
	}
	if !queueObservation.acquire(r.now()) {
		return
	}
	var pending int64
	var processing int64
	var oldestAgeSeconds float64
	if err := r.pool.QueryRow(ctx, mustSQL("queue_observability.sql")).Scan(
		&pending,
		&processing,
		&oldestAgeSeconds,
	); err != nil {
		return
	}
	youtubePendingQueue.Set(float64(pending))
	youtubeProcessingQueue.Set(float64(processing))
	youtubeQueueOldestAgeSeconds.Set(oldestAgeSeconds)
}
