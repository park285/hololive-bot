package dispatchoutbox

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *PgxRepository) InsertShadowed(ctx context.Context, envelope domain.AlarmQueueEnvelope) (*Record, error) {
	result, err := r.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusShadowed})
	if err != nil {
		return nil, err
	}
	if result.InsertedDeliveries == 0 {
		return r.findByDedupeKeyAny(ctx, BuildDedupeKeyFromEnvelope(envelope), BuildLegacyDedupeKeyFromEnvelope(envelope))
	}
	return r.findByDedupeKeyAny(ctx, BuildDedupeKeyFromEnvelope(envelope), BuildLegacyDedupeKeyFromEnvelope(envelope))
}

func (r *PgxRepository) InsertPending(ctx context.Context, envelope domain.AlarmQueueEnvelope) (*Record, InsertResult, error) {
	result, err := r.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{envelope}, Status: StatusPending})
	if err != nil {
		return nil, "", err
	}
	record, err := r.findByDedupeKeyAny(ctx, BuildDedupeKeyFromEnvelope(envelope), BuildLegacyDedupeKeyFromEnvelope(envelope))
	if err != nil {
		return nil, "", err
	}
	if result.InsertedDeliveries > 0 {
		return record, Inserted, nil
	}
	switch record.Status {
	case StatusShadowed:
		return record, DuplicateShadowed, nil
	case StatusSent, StatusDLQ, StatusQuarantined, StatusCancelled:
		return record, DuplicateTerminal, nil
	default:
		return record, DuplicateActive, nil
	}
}

func (r *PgxRepository) InsertBatch(ctx context.Context, input PublishBatchInput) (PublishBatchResult, error) {
	if r == nil || r.pool == nil {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: postgres pool is nil")
	}
	status := input.Status
	if status == "" {
		status = StatusPending
	}
	if status != StatusPending && status != StatusShadowed {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: unsupported status %q", status)
	}
	result := PublishBatchResult{RequestedDeliveries: len(input.Envelopes)}
	if len(input.Envelopes) == 0 {
		return result, nil
	}

	eventRows, deliveries, preflightCollisions, result, err := prepareInsertBatchRows(input.Envelopes, status, result)
	if err != nil {
		return result, err
	}
	return r.insertPreparedBatch(ctx, eventRows, deliveries, preflightCollisions, result)
}
