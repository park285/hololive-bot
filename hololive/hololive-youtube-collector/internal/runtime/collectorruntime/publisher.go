package collectorruntime

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

type Publisher struct {
	pool         *pgxpool.Pool
	observations *sourceobservation.PublishRepository
}

func NewPublisher(pool *pgxpool.Pool) *Publisher {
	return &Publisher{pool: pool, observations: sourceobservation.NewPublishRepository(pool)}
}

func (p *Publisher) LoadContractGenerations(
	ctx context.Context,
	provider contract.Provider,
	kinds []contract.ObservationKind,
) (map[contract.ObservationKind]int64, error) {
	if p == nil || p.pool == nil || !provider.Valid() || len(kinds) == 0 {
		return nil, collecterr.New(collecterr.Failed, "load observation contract generations: request is invalid")
	}
	values := make([]string, len(kinds))
	for i := range kinds {
		values[i] = string(kinds[i])
	}
	rows, err := p.pool.Query(ctx, mustSQL("load_contract_generations.sql"), string(provider), values)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("load observation contract generations: %w", err))
	}
	defer rows.Close()
	result, err := scanContractGenerations(rows, len(kinds))
	if err != nil {
		return nil, err
	}
	return requireContractGenerations(result, kinds)
}

func scanContractGenerations(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}, size int) (map[contract.ObservationKind]int64, error) {
	result := make(map[contract.ObservationKind]int64, size)
	for rows.Next() {
		var kind contract.ObservationKind
		var generation int64
		if err := rows.Scan(&kind, &generation); err != nil {
			return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("scan observation contract generation: %w", err))
		}
		if generation <= 0 {
			return nil, collecterr.New(collecterr.Failed, "observation contract generation must be positive")
		}
		result[kind] = generation
	}
	if err := rows.Err(); err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, fmt.Errorf("read observation contract generations: %w", err))
	}
	return result, nil
}

func requireContractGenerations(result map[contract.ObservationKind]int64, kinds []contract.ObservationKind) (map[contract.ObservationKind]int64, error) {
	for _, kind := range kinds {
		if _, ok := result[kind]; !ok {
			return nil, collecterr.New(collecterr.Failed, "observation contract generation is missing for "+string(kind))
		}
	}
	return result, nil
}

func (p *Publisher) Publish(ctx context.Context, lease contract.LeaseProof, output collectutil.RunOutput) (sourceobservation.PublishBatchResult, error) {
	if p == nil || p.observations == nil {
		return sourceobservation.PublishBatchResult{}, collecterr.New(collecterr.Failed, "observation publisher is not configured")
	}
	if len(output.Observations) == 0 {
		return sourceobservation.PublishBatchResult{}, nil
	}
	if len(output.Checkpoints) != len(output.Observations) {
		return sourceobservation.PublishBatchResult{}, collecterr.New(collecterr.ParserDrift, "checkpoint count does not match observation count")
	}
	result, err := p.observations.PublishBatch(ctx, sourceobservation.PublishBatchInput{
		Lease: lease,
		Checkpoint: sourceobservation.CheckpointUpdate{
			Entries:           output.Checkpoints,
			CollectionLatency: output.CollectionLatency,
		},
		Observations: output.Observations,
	})
	if err != nil {
		return sourceobservation.PublishBatchResult{}, collecterr.Wrap(collecterr.PublishRejected, fmt.Errorf("publish observation batch: %w", err))
	}
	return result, nil
}
