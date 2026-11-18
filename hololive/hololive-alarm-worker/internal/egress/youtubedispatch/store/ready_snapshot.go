package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var expiredPendingBounded = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "hololive_youtube_delivery_expired_pending_bounded",
	Help: "Freshness를 지난 PENDING delivery 수. 기존 batch size까지만 집계한다.",
})

func SampleQueue(ctx context.Context, pool *pgxpool.Pool, lockTimeout, freshness time.Duration, batchSize int) (workercontract.QueueValues, error) {
	values, err := ReadySnapshot(ctx, pool, lockTimeout, freshness, batchSize)
	if err != nil {
		return workercontract.QueueValues{}, err
	}

	expired, err := expiredPendingCount(ctx, pool, freshness, batchSize)
	if err != nil {
		return workercontract.QueueValues{}, err
	}

	expiredPendingBounded.Set(float64(expired))

	return values, nil
}

func ReadySnapshot(ctx context.Context, pool *pgxpool.Pool, lockTimeout, freshness time.Duration, batchSize int) (workercontract.QueueValues, error) {
	var (
		depth      int64
		ageSeconds float64
	)

	if err := pool.QueryRow(
		ctx, deliveryReadySnapshotSQL, "PENDING", lockTimeout.Milliseconds(), freshness.Milliseconds(), batchSize,
	).Scan(&depth, &ageSeconds); err != nil {
		return workercontract.QueueValues{}, fmt.Errorf("sample ready youtube deliveries: %w", err)
	}

	return workercontract.QueueValues{Depth: depth, OldestQueuedAge: time.Duration(ageSeconds * float64(time.Second))}, nil
}

func expiredPendingCount(ctx context.Context, pool *pgxpool.Pool, freshness time.Duration, batchSize int) (int64, error) {
	var expired int64

	if err := pool.QueryRow(ctx, deliveryExpiredSnapshotSQL, freshness.Milliseconds(), batchSize).Scan(&expired); err != nil {
		return 0, fmt.Errorf("sample expired youtube deliveries: %w", err)
	}

	return expired, nil
}
