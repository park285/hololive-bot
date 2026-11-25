package dispatchoutbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

func ReadySnapshot(ctx context.Context, pool *pgxpool.Pool) (workercontract.QueueValues, error) {
	var (
		depth      int64
		ageSeconds float64
	)

	if err := pgxdb.QueryRow(ctx, pool, deliveryReadySnapshotSQL).Scan(&depth, &ageSeconds); err != nil {
		return workercontract.QueueValues{}, fmt.Errorf("sample ready alarm deliveries: %w", err)
	}

	return workercontract.QueueValues{Depth: depth, OldestQueuedAge: time.Duration(ageSeconds * float64(time.Second))}, nil
}
