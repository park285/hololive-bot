package runtime

import "context"

func (r *Runtime) observePendingQueue(ctx context.Context) {
	if r == nil || r.pool == nil {
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
	if cap(r.workCh) > 0 {
		youtubeWorkQueueUtilization.Set(float64(len(r.workCh)) / float64(cap(r.workCh)))
	}
}
