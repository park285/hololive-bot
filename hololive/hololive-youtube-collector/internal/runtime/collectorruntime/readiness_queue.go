package collectorruntime

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type postgresQueueStore struct {
	pool *pgxpool.Pool
}

func queueStoreFrom(infra *collectorInfrastructure) queueStore {
	if infra == nil || infra.postgres == nil || infra.postgres.GetPool() == nil {
		return nil
	}
	return &postgresQueueStore{pool: infra.postgres.GetPool()}
}

func (s *postgresQueueStore) LoadHandoffStatuses(ctx context.Context, ids []int64) ([]handoffStatus, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("load observation handoff status: postgres pool is required")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxHandoffCandidates {
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "handoff status request exceeds candidate cap")
	}
	rows, err := s.pool.Query(ctx, mustSQL("observation_handoff_status.sql"), ids)
	if err != nil {
		return nil, fmt.Errorf("load observation handoff status: %w", err)
	}
	defer rows.Close()
	return scanHandoffStatuses(rows, ids)
}

func (s *postgresQueueStore) CountPending(ctx context.Context, limit int) (BoundedCount, error) {
	if s == nil || s.pool == nil {
		return BoundedCount{}, fmt.Errorf("count pending source observations: postgres pool is required")
	}
	if limit < 1 {
		return BoundedCount{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "pending queue cap is invalid")
	}
	var value int
	if err := s.pool.QueryRow(ctx, mustSQL("pending_observation_count.sql"), limit).Scan(&value); err != nil {
		return BoundedCount{}, fmt.Errorf("count pending source observations: %w", err)
	}
	return newBoundedCount(value, limit)
}

func scanHandoffStatuses(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}, ids []int64) ([]handoffStatus, error) {
	out := make([]handoffStatus, 0, len(ids))
	for rows.Next() {
		item, err := scanHandoffRow(rows)
		if err != nil {
			return nil, err
		}
		if len(out) >= len(ids) {
			return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "observation handoff status row count is invalid")
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read observation handoff status: %w", err)
	}
	if err := requireHandoffShape(ids, out); err != nil {
		return nil, err
	}
	return out, nil
}

func scanHandoffRow(rows interface{ Scan(dest ...any) error }) (handoffStatus, error) {
	var id int64
	var raw *string
	if err := rows.Scan(&id, &raw); err != nil {
		return handoffStatus{}, fmt.Errorf("scan observation handoff status: %w", err)
	}
	if raw == nil {
		return handoffStatus{ObservationID: id}, nil
	}
	status := contract.Status(*raw)
	if !status.Valid() {
		return handoffStatus{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "observation handoff status is unknown")
	}
	return handoffStatus{ObservationID: id, Status: &status}, nil
}
