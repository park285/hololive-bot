package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

// ReadySnapshot은 request leader가 아니라 claim 가능한 delivery 행 수를 반환한다.
// 신선도는 부모 outbox.created_at, 대기 시간은 retry/만료 lock의 예약 임계값이다.
func ReadySnapshot(ctx context.Context, pool *pgxpool.Pool, lockTimeout, freshness time.Duration, batchSize int) (workercontract.QueueValues, error) {
	at := time.Now().UTC().Truncate(time.Microsecond)

	var (
		depth      int64
		ageSeconds float64
	)

	if err := pool.QueryRow(ctx, readySnapshotSQL, "PENDING", at.Add(-lockTimeout), at, at.Add(-freshness), batchSize).Scan(&depth, &ageSeconds); err != nil {
		return workercontract.QueueValues{}, fmt.Errorf("sample ready youtube deliveries: %w", err)
	}

	return workercontract.QueueValues{Depth: depth, OldestQueuedAge: time.Duration(ageSeconds * float64(time.Second))}, nil
}
