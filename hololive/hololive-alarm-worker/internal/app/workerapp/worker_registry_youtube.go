package workerapp

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
)

//go:embed queries/youtube_delivery_expired_snapshot.sql
var youtubeDeliveryExpiredSnapshotSQL string

var youtubeExpiredPending = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_youtube_delivery_expired_pending_bounded",
	Help: "Freshness를 지난 PENDING delivery 수. 기존 batch size까지만 집계한다.",
})

func newYouTubeQueueSampler(pool *pgxpool.Pool, lockTimeout, freshness time.Duration, batchSize int) *workercontract.QueueSampler {
	return workercontract.NewQueueSampler(func(ctx context.Context) (workercontract.QueueValues, error) {
		values, err := store.ReadySnapshot(ctx, pool, lockTimeout, freshness, batchSize)
		if err != nil {
			return workercontract.QueueValues{}, fmt.Errorf("sample ready youtube deliveries: %w", err)
		}

		var expired int64

		if err := pool.QueryRow(ctx, youtubeDeliveryExpiredSnapshotSQL, freshness.Milliseconds(), batchSize).Scan(&expired); err != nil {
			return workercontract.QueueValues{}, fmt.Errorf("sample expired youtube deliveries: %w", err)
		}

		youtubeExpiredPending.Set(float64(expired))

		return values, nil
	})
}
