package runtime

import (
	"context"
	"sync/atomic"
	"time"
)

const (
	queueObservationMinInterval = 5 * time.Second
	queueObservationTimeout     = time.Second
)

type queueObservationThrottle struct {
	last atomic.Int64
}

func (t *queueObservationThrottle) acquireEvery(now time.Time, interval time.Duration) bool {
	last := t.last.Load()
	if elapsed := now.Sub(time.Unix(0, last)); elapsed >= 0 && elapsed < interval {
		return false
	}

	return t.last.CompareAndSwap(last, now.UnixNano())
}

func (r *Runtime) runQueueObservationLoop(ctx context.Context) {
	ticker := time.NewTicker(queueObservationMinInterval)
	defer ticker.Stop()

	r.observePendingQueue(ctx)

	for waitTicker(ctx, ticker) {
		r.observePendingQueue(ctx)
	}
}

func (r *Runtime) observePendingQueue(ctx context.Context) {
	if r == nil || r.pool == nil {
		return
	}

	if r.collectionObservation.acquireEvery(r.now(), 30*time.Second) {
		r.observeCollectionTargets(ctx)
	}

	if cap(r.workCh) > 0 {
		youtubeWorkQueueUtilization.Set(float64(len(r.workCh)) / float64(cap(r.workCh)))
	}

	var (
		pending          int64
		processing       int64
		oldestAgeSeconds float64
		deadLetterExists bool
	)

	queryCtx, cancel := context.WithTimeout(ctx, queueObservationTimeout)
	defer cancel()

	err := r.withDB(queryCtx, dbOperationQueueSnapshot, func(ctx context.Context) error {
		return r.pool.QueryRow(ctx, queueObservabilitySQL).Scan(&pending, &processing, &oldestAgeSeconds, &deadLetterExists)
	})

	recordQueueObservation(pending, processing, oldestAgeSeconds, r.now(), err)

	if err == nil {
		r.observeDeadLetterBacklog(deadLetterExists)
	}
}

func (r *Runtime) observeDeadLetterBacklog(exists bool) {
	if r.deadLetterBacklog.Swap(exists) != exists && r.started.Load() {
		r.publishHealth()
	}
}

func recordQueueObservation(pending, processing int64, oldestAgeSeconds float64, observedAt time.Time, err error) {
	if err != nil {
		youtubeQueueSnapshotSuccess.Set(0)

		return
	}

	youtubePendingQueue.Set(float64(pending))
	youtubeProcessingQueue.Set(float64(processing))
	youtubeQueueOldestAgeSeconds.Set(oldestAgeSeconds)
	youtubeQueueSnapshotLastSuccess.Set(float64(observedAt.Unix()))
	youtubeQueueSnapshotSuccess.Set(1)
}
