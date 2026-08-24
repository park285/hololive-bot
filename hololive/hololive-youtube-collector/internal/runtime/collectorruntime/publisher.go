package collectorruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

type ContractGenerationReader interface {
	LoadContractGenerations(context.Context, contract.Provider, []contract.ObservationKind) (map[contract.ObservationKind]int64, error)
}

type ObservationPublisher interface {
	PublishBatch(context.Context, *sourceobservation.PublishBatchInput) (sourceobservation.PublishBatchResult, error)
	PublishBatchAndDefer(context.Context, *sourceobservation.PublishBatchInput, sourceobservation.DeferCollectionInput) (sourceobservation.PublishBatchResult, error)
}

type Publisher struct {
	contracts    ContractGenerationReader
	observations ObservationPublisher
}

func NewPublisher(pool *pgxpool.Pool) *Publisher {
	return &Publisher{contracts: &postgresContractGenerationReader{pool: pool}, observations: sourceobservation.NewPublishRepository(pool)}
}

func NewPublisherWithStores(contracts ContractGenerationReader, observations ObservationPublisher) (*Publisher, error) {
	if contracts == nil || observations == nil {
		return nil, errors.New("create collector publisher: stores are required")
	}

	return &Publisher{contracts: contracts, observations: observations}, nil
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
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "load observation contract generations: request is invalid")
	}

	values := make([]string, len(kinds))
	for i := range kinds {
		values[i] = string(kinds[i])
	}

	rows, err := p.pool.Query(ctx, mustSQL("load_contract_generations.sql"), string(provider), values)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, collecterr.ClassTransient, fmt.Errorf("load observation contract generations: %w", err))
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

func (p *Publisher) LoadContractSnapshot(ctx context.Context, registration RegisteredRunner) (collectutil.ContractSnapshot, error) {
	if p == nil || p.contracts == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collectutil.ContractSnapshot{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "contract generation reader is not configured")
	}

	job := registration.Contract()
	emissions := job.Emissions()

	values, err := p.contracts.LoadContractGenerations(ctx, job.ID().Provider, emissions)
	if err != nil {
		return collectutil.ContractSnapshot{}, fmt.Errorf("load contract generations: %w", err)
	}

	out, err := collectutil.NewContractSnapshot(emissions, values)
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
			return nil, collecterr.Wrap(collecterr.Failed, collecterr.ClassTransient, fmt.Errorf("scan observation contract generation: %w", err))
		}

		if generation <= 0 {
			//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
			return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "observation contract generation must be positive")
		}

		result[kind] = generation
	}

	if err := rows.Err(); err != nil {
		return nil, collecterr.Wrap(collecterr.Failed, collecterr.ClassTransient, fmt.Errorf("read observation contract generations: %w", err))
	}

	return result, nil
}

func requireContractGenerations(result map[contract.ObservationKind]int64, kinds []contract.ObservationKind) (map[contract.ObservationKind]int64, error) {
	for _, kind := range kinds {
		if _, ok := result[kind]; !ok {
			//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
			return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "observation contract generation is missing for "+string(kind))
		}
	}

	return result, nil
}

func (p *Publisher) PublishComplete(ctx context.Context, lease *contract.LeaseProof, output collectutil.RunOutput) (sourceobservation.PublishBatchResult, error) {
	if p == nil || p.observations == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return sourceobservation.PublishBatchResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "observation publisher is not configured")
	}

	if lease == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return sourceobservation.PublishBatchResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "complete publish lease is missing")
	}

	observations := output.Observations()
	checkpoints := output.Checkpoints()

	if len(observations) == 0 {
		return sourceobservation.PublishBatchResult{}, nil
	}

	if len(checkpoints) != len(observations) {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return sourceobservation.PublishBatchResult{}, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "checkpoint count does not match observation count")
	}

	input := &sourceobservation.PublishBatchInput{
		Lease: *lease,
		Checkpoint: sourceobservation.CheckpointUpdate{
			Entries:           checkpoints,
			CollectionLatency: output.CollectionLatency(),
		},
		Observations: observations,
	}

	result, err := p.observations.PublishBatch(ctx, input)
	if err != nil {
		return sourceobservation.PublishBatchResult{}, fmt.Errorf("wrap publish failure: %w", wrapPublishFailure("publish observation batch", err))
	}

	if err := sourceobservation.ValidatePublishBatchResult(len(observations), result); err != nil {
		return sourceobservation.PublishBatchResult{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err)
	}

	return result, nil
}

func (p *Publisher) PublishPartial(
	ctx context.Context,
	lease *contract.LeaseProof,
	result *collectutil.CollectResult,
	retry joblease.RetryDecision,
	bounds sourceobservation.RetryBounds,
) (sourceobservation.PublishBatchResult, error) {
	partial, err := validatePartialPublishInput(p, lease, result)
	if err != nil {
		return sourceobservation.PublishBatchResult{}, fmt.Errorf("validate partial publish input: %w", err)
	}

	output := result.Output()

	schedule, err := retrySchedule(retry)
	if err != nil {
		return sourceobservation.PublishBatchResult{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err)
	}

	deferInput, err := sourceobservation.NewDeferCollectionInput(collecterr.DiagnosticOf(partial.Cause()), bounds, schedule)
	if err != nil {
		return sourceobservation.PublishBatchResult{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err)
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
	result *collectutil.CollectResult,
) (*collectutil.PartialFailure, error) {
	partial, ok := result.PartialFailure()
	if publisher == nil || publisher.observations == nil || lease == nil || !ok {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return nil, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "partial publish input is invalid")
	}

	return partial, nil
}

func retrySchedule(retry joblease.RetryDecision) (sourceobservation.RetrySchedule, error) {
	switch retry.Kind() {
	case joblease.RetryDecisionDelay:
		out, err := retryDelaySchedule(retry)

		return out, errors.Join(err)
	case joblease.RetryDecisionAt:
		out, err := retryAtSchedule(retry)

		return out, errors.Join(err)
	default:
		err := validateEmptyRetrySchedule(retry)

		return sourceobservation.RetrySchedule{}, errors.Join(err)
	}
}

func retryDelaySchedule(retry joblease.RetryDecision) (sourceobservation.RetrySchedule, error) {
	delay, _ := retry.Delay()

	out, err := sourceobservation.NewRetryDelaySchedule(delay)
	if err != nil {
		return out, fmt.Errorf("retry delay schedule: %w", err)
	}

	return out, nil
}

func retryAtSchedule(retry joblease.RetryDecision) (sourceobservation.RetrySchedule, error) {
	at, _ := retry.At()

	out, err := sourceobservation.NewRetryAtSchedule(at)
	if err != nil {
		return out, fmt.Errorf("retry at schedule: %w", err)
	}

	return out, nil
}

func validateEmptyRetrySchedule(retry joblease.RetryDecision) error {
	if err := retry.Validate(); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	return nil
}

func publishBatchInput(lease *contract.LeaseProof, output collectutil.RunOutput) *sourceobservation.PublishBatchInput {
	return &sourceobservation.PublishBatchInput{
		Lease: *lease,
		Checkpoint: sourceobservation.CheckpointUpdate{
			Entries: output.Checkpoints(), CollectionLatency: output.CollectionLatency(),
		},
		Observations: output.Observations(),
	}
}

func (p *Publisher) publishPartialBatch(
	ctx context.Context,
	input *sourceobservation.PublishBatchInput,
	deferInput sourceobservation.DeferCollectionInput,
) (sourceobservation.PublishBatchResult, error) {
	published, err := p.observations.PublishBatchAndDefer(ctx, input, deferInput)
	if err != nil {
		if wrapPublishErr := wrapPublishFailure("publish partial observation batch", err); wrapPublishErr != nil {
			return sourceobservation.PublishBatchResult{}, fmt.Errorf("wrap publish failure: %w", wrapPublishErr)
		}

		return sourceobservation.PublishBatchResult{}, nil
	}

	if err := sourceobservation.ValidatePublishBatchResult(len(input.Observations), published); err != nil {
		return sourceobservation.PublishBatchResult{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err)
	}

	return published, nil
}

func wrapPublishFailure(action string, err error) error {
	code := collecterr.PublishRejected
	class := collecterr.ClassTransient

	switch {
	case errors.Is(err, sourceobservation.ErrInvalidEnvelope):
		code = collecterr.Internal
		class = collecterr.ClassInternal
	case errors.Is(err, sourceobservation.ErrStaleContract):
		class = collecterr.ClassProtocol
	}

	return collecterr.Wrap(code, class, fmt.Errorf("%s: %w", action, err))
}
