package workerapp

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
)

func newYouTubeQueueSampler(pool *pgxpool.Pool, lockTimeout, freshness time.Duration, batchSize int) *workercontract.QueueSampler {
	return workercontract.NewQueueSampler(func(ctx context.Context) (workercontract.QueueValues, error) {
		return store.SampleQueue(ctx, pool, lockTimeout, freshness, batchSize)
	})
}
