package collectorruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-collector/internal/runtime/collection"
	"github.com/kapu/hololive-collector/internal/runtime/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type Publisher struct {
	contracts    *postgresContractGenerationReader
	observations *sourceobservation.Repository
}

func NewPublisher(pool *pgxpool.Pool) *Publisher {
	return &Publisher{contracts: &postgresContractGenerationReader{pool: pool}, observations: sourceobservation.NewRepository(pool)}
}

type postgresContractGenerationReader struct {
	pool *pgxpool.Pool
}

func (p *postgresContractGenerationReader) LoadContractGenerations(
	ctx context.Context,
	provider contract.Provider,
	kinds []contract.ObservationKind,
) (map[contract.ObservationKind]int64, error) {
	if p == nil || p.pool == nil || !provider.Valid() || len(kinds) == 0 {
		return nil, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "load observation contract generations: request is invalid")
	}

	values := make([]string, len(kinds))
	for i := range kinds {
		values[i] = string(kinds[i])
	}

	rows, err := pgxdb.Query(ctx, p.pool, loadContractGenerationsSQL, string(provider), values)
	if err != nil {
		return nil, collecterr.Wrap(contract.ErrorCollectionFailed, contract.ClassTransient, fmt.Errorf("load observation contract generations: %w", err))
	}
	defer rows.Close()

	result, err := scanContractGenerations(rows, len(kinds))
	if err != nil {
		return nil, fmt.Errorf("scan contract generations: %w", err)
	}

	out, err := requireContractGenerations(result, kinds)
	if err != nil {
		return nil, fmt.Errorf("require contract generations: %w", err)
	}

	return out, nil
}

func (p *Publisher) LoadContractSnapshot(ctx context.Context, registration RegisteredRunner) (collection.ContractSnapshot, error) {
	if p == nil || p.contracts == nil {
		return collection.ContractSnapshot{}, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "contract generation reader is not configured")
	}

	job := registration.Contract()
	emissions := job.Emissions()

	values, err := p.contracts.LoadContractGenerations(ctx, job.ID().Provider, emissions)
	if err != nil {
		return collection.ContractSnapshot{}, fmt.Errorf("load contract generations: %w", err)
	}

	out, err := collection.NewContractSnapshot(emissions, values)
	if err != nil {
		return out, fmt.Errorf("contract snapshot: %w", err)
	}

	return out, nil
}

func scanContractGenerations(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}, size int,
) (map[contract.ObservationKind]int64, error) {
	result := make(map[contract.ObservationKind]int64, size)

	for rows.Next() {
		var (
			kind       contract.ObservationKind
			generation int64
		)

		if err := rows.Scan(&kind, &generation); err != nil {
			return nil, collecterr.Wrap(contract.ErrorCollectionFailed, contract.ClassTransient, fmt.Errorf("scan observation contract generation: %w", err))
		}

		if generation <= 0 {
			return nil, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "observation contract generation must be positive")
		}

		result[kind] = generation
	}

	if err := rows.Err(); err != nil {
		return nil, collecterr.Wrap(contract.ErrorCollectionFailed, contract.ClassTransient, fmt.Errorf("read observation contract generations: %w", err))
	}

	return result, nil
}

func requireContractGenerations(result map[contract.ObservationKind]int64, kinds []contract.ObservationKind) (map[contract.ObservationKind]int64, error) {
	for _, kind := range kinds {
		if _, ok := result[kind]; !ok {
			return nil, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "observation contract generation is missing for "+string(kind))
		}
	}

	return result, nil
}

func (p *Publisher) PublishComplete(ctx context.Context, lease *contract.LeaseProof, output collection.RunOutput) (sourceobservation.PublishBatchResult, error) {
	if p == nil || p.observations == nil {
		return sourceobservation.PublishBatchResult{}, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "observation publisher is not configured")
	}

	if lease == nil {
		return sourceobservation.PublishBatchResult{}, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "complete publish lease is missing")
	}

	if output.Empty() {
		return sourceobservation.PublishBatchResult{}, nil
	}

	input := publishBatchInput(lease, output)

	result, err := p.observations.PublishBatch(ctx, input)
	if err != nil {
		return sourceobservation.PublishBatchResult{}, fmt.Errorf("wrap publish failure: %w", wrapPublishFailure("publish observation batch", err))
	}

	return result, nil
}

func (p *Publisher) PublishPartial(
	ctx context.Context,
	lease *contract.LeaseProof,
	result *collection.CollectResult,
	schedule collection.RetrySchedule,
	bounds collection.RetryBounds,
) (sourceobservation.PublishBatchResult, error) {
	partial, err := validatePartialPublishInput(p, lease, result)
	if err != nil {
		return sourceobservation.PublishBatchResult{}, fmt.Errorf("validate partial publish input: %w", err)
	}

	output := result.Output()

	deferInput, err := collection.NewDeferCollectionInput(collecterr.DiagnosticOf(partial.Cause()), bounds, schedule)
	if err != nil {
		return sourceobservation.PublishBatchResult{}, collecterr.Wrap(contract.ErrorInternalInvariant, contract.ClassInternal, err)
	}

	input := publishBatchInput(lease, output)

	out, err := p.publishPartialBatch(ctx, input, deferInput)
	if err != nil {
		return out, fmt.Errorf("publish partial batch: %w", err)
	}

	return out, nil
}

func validatePartialPublishInput(
	publisher *Publisher,
	lease *contract.LeaseProof,
	result *collection.CollectResult,
) (*collection.PartialFailure, error) {
	partial, ok := result.PartialFailure()
	if publisher == nil || publisher.observations == nil || lease == nil || !ok {
		return nil, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "partial publish input is invalid")
	}

	return partial, nil
}

func publishBatchInput(lease *contract.LeaseProof, output collection.RunOutput) *sourceobservation.PublishBatchInput {
	observations := output.Observations()
	entries := make([]sourceobservation.CheckpointEntry, len(observations))

	for i := range observations {
		envelope := &observations[i]

		entries[i] = sourceobservation.CheckpointEntry{
			Provider:           envelope.Provider,
			ObservationKind:    envelope.ObservationKind,
			SubjectKey:         envelope.SubjectKey,
			ScopeSHA256:        envelope.ScopeSHA256,
			ContractGeneration: envelope.ContractGeneration,
			LastObservationKey: envelope.ObservationKey,
			LastEvidenceSHA256: envelope.EvidenceSHA256,
			LastScheduledFor:   envelope.ScheduledFor,
			Continuity:         envelope.Continuity,
			Cursor:             output.Cursor(i),
		}
	}

	return &sourceobservation.PublishBatchInput{
		Lease: *lease,
		Checkpoint: sourceobservation.CheckpointUpdate{
			Entries: entries, CollectionLatency: output.CollectionLatency(),
		},
		Observations: observations,
	}
}

func (p *Publisher) publishPartialBatch(
	ctx context.Context,
	input *sourceobservation.PublishBatchInput,
	deferInput collection.DeferCollectionInput,
) (sourceobservation.PublishBatchResult, error) {
	published, err := p.observations.PublishBatchAndDefer(ctx, input, deferInput)
	if err != nil {
		return sourceobservation.PublishBatchResult{}, wrapPublishFailure("publish partial observation batch", err)
	}

	return published, nil
}

func wrapPublishFailure(action string, err error) error {
	code := contract.ErrorPublishRejected
	class := contract.ClassTransient

	switch {
	case errors.Is(err, contract.ErrPayloadTooLarge):
		code = contract.ErrorResponseTooLarge
		class = contract.ClassResourceLimit
	case errors.Is(err, sourceobservation.ErrInvalidEnvelope):
		code = contract.ErrorInternalInvariant
		class = contract.ClassInternal
	case errors.Is(err, sourceobservation.ErrStaleContract):
		class = contract.ClassProtocol
	}

	return collecterr.Wrap(code, class, fmt.Errorf("%s: %w", action, err))
}
