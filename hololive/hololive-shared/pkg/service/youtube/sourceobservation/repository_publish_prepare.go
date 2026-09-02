package sourceobservation

import (
	"errors"
	"fmt"
	"slices"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func clonePublishBatchInput(input *PublishBatchInput) (PublishBatchInput, error) {
	if input == nil {
		return PublishBatchInput{}, fmt.Errorf("publish source observation batch: %w: input is nil", ErrInvalidEnvelope)
	}

	cloned := PublishBatchInput{
		Lease: input.Lease,
		Checkpoint: CheckpointUpdate{
			Entries:           make([]CheckpointEntry, len(input.Checkpoint.Entries)),
			CollectionLatency: input.Checkpoint.CollectionLatency,
		},
		Observations: make([]contract.Envelope, len(input.Observations)),
	}
	for i := range input.Observations {
		cloned.Observations[i] = clonePublishEnvelope(&input.Observations[i])
	}

	for i := range input.Checkpoint.Entries {
		cloned.Checkpoint.Entries[i] = clonePublishCheckpoint(&input.Checkpoint.Entries[i])
	}

	return cloned, nil
}

func clonePublishEnvelope(src *contract.Envelope) contract.Envelope {
	dst := *src

	dst.Payload = slices.Clone(src.Payload)

	if src.SourceEventAt != nil {
		timestamp := *src.SourceEventAt

		dst.SourceEventAt = &timestamp
	}

	return dst
}

func clonePublishCheckpoint(src *CheckpointEntry) CheckpointEntry {
	dst := *src

	dst.Cursor = slices.Clone(src.Cursor)

	return dst
}

func preparePublishBatch(input *PublishBatchInput) (preparedPublishBatch, error) {
	if err := preflightPublishBatch(input); err != nil {
		return preparedPublishBatch{}, fmt.Errorf("publish source observation batch: %w", err)
	}

	cloned, err := clonePublishBatchInput(input)
	if err != nil {
		return preparedPublishBatch{}, fmt.Errorf("clone publish batch input: %w", err)
	}

	if validateErr := validatePublishBatch(&cloned); validateErr != nil {
		return preparedPublishBatch{}, fmt.Errorf("publish source observation batch: %w", validateErr)
	}

	observations, contracts, err := encodePublishBatch(&cloned)
	if err != nil {
		return preparedPublishBatch{}, fmt.Errorf("encode publish batch: %w", err)
	}

	return preparedPublishBatch{input: cloned, observations: observations, contracts: contracts}, nil
}

func preflightPublishBatch(input *PublishBatchInput) error {
	if input == nil {
		return fmt.Errorf("%w: input is nil", ErrInvalidEnvelope)
	}

	if err := validatePublishBatchCounts(input); err != nil {
		return fmt.Errorf("validate publish batch counts: %w", err)
	}

	aggregateBytes := 0
	if err := preflightPublishObservations(input.Observations, &aggregateBytes); err != nil {
		return fmt.Errorf("preflight publish observations: %w", err)
	}

	if err := preflightPublishCheckpoints(input.Checkpoint.Entries, &aggregateBytes); err != nil {
		return fmt.Errorf("preflight publish checkpoints: %w", err)
	}

	return nil
}

func preflightPublishObservations(observations []contract.Envelope, aggregateBytes *int) error {
	for i := range observations {
		if len(observations[i].Payload) > contract.MaxPayloadBytes {
			return fmt.Errorf("%w: observation %d payload is too large", ErrInvalidEnvelope, i)
		}

		if !publishBytesWithinLimit(aggregateBytes, len(observations[i].Payload)) {
			return publishBatchBytesError()
		}
	}

	return nil
}

func preflightPublishCheckpoints(checkpoints []CheckpointEntry, aggregateBytes *int) error {
	for i := range checkpoints {
		if err := validateCheckpointCursorSize(checkpoints[i].Cursor, i); err != nil {
			return fmt.Errorf("validate checkpoint cursor size: %w", err)
		}

		if !publishBytesWithinLimit(aggregateBytes, len(checkpoints[i].Cursor)) {
			return publishBatchBytesError()
		}
	}

	return nil
}

func publishBytesWithinLimit(aggregateBytes *int, next int) bool {
	if next > MaxPublishBatchBytes-*aggregateBytes {
		return false
	}

	*aggregateBytes += next

	return true
}

func publishBatchBytesError() error {
	return fmt.Errorf(
		"%w: aggregate payload and cursor bytes exceed %d",
		ErrInvalidEnvelope,
		MaxPublishBatchBytes,
	)
}

func ValidatePublishBatchResult(want int, result PublishBatchResult) error {
	if len(result.Results) != want {
		return errors.New("publish source observation batch: invalid set result")
	}

	seen := make([]bool, want)

	for i := range result.Results {
		item := result.Results[i]
		if item.Ordinal != i || item.ObservationID <= 0 || !validPublishOutcome(item.Outcome) || seen[i] {
			return errors.New("publish source observation batch: invalid set result")
		}

		seen[i] = true
	}

	return nil
}
