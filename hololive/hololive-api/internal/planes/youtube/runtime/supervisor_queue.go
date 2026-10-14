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

// 통계 지연은 이미 선점한 작업의 전달을 막지 않는다. Supervisor가 이 loop의 취소와 join을 소유한다.
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
	)

	queryCtx, cancel := context.WithTimeout(ctx, queueObservationTimeout)
	defer cancel()

	err := r.withDB(queryCtx, dbOperationQueueSnapshot, func(ctx context.Context) error {
		return r.pool.QueryRow(ctx, mustSQL("queue_observability.sql")).Scan(&pending, &processing, &oldestAgeSeconds)
	})

	recordQueueObservation(pending, processing, oldestAgeSeconds, r.now(), err)
}

// 실패는 이전 깊이·경과를 덮지 않고 success만 내린다. 5초 주기라 로그 대신 상태 gauge와 마지막 성공 시각으로 드러낸다.
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
