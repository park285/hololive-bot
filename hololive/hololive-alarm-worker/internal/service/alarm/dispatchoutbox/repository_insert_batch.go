package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

func (r *PgxRepository) InsertBatch(ctx context.Context, input PublishBatchInput) (PublishBatchResult, error) {
	if r == nil || r.pool == nil {
		return PublishBatchResult{}, errors.New("insert dispatch ledger batch: postgres pool is nil")
	}

	result := PublishBatchResult{RequestedDeliveries: len(input.Envelopes)}
	if len(input.Envelopes) == 0 {
		return result, nil
	}

	eventRows, deliveries, preflightCollisions, err := prepareInsertBatchRows(input.Envelopes, &result)
	if err != nil {
		return result, fmt.Errorf("prepare insert batch rows: %w", err)
	}

	out, err := runPublishBatchWithDeadlockRetry(&result, func() (PublishBatchResult, error) {
		return r.insertPreparedBatch(ctx, eventRows, deliveries, preflightCollisions, input.Envelopes, &result)
	})
	if err != nil {
		return out, fmt.Errorf("run publish batch with deadlock retry: %w", err)
	}

	return out, nil
}

const pgErrCodeDeadlockDetected = "40P01"

func runPublishBatchWithDeadlockRetry(
	result *PublishBatchResult,
	attempt func() (PublishBatchResult, error),
) (PublishBatchResult, error) {
	snapshot := *result
	publishResult, err := attempt()

	if !isDeadlockDetected(err) {
		if err != nil {
			return publishResult, fmt.Errorf("attempt: %w", err)
		}

		return publishResult, nil
	}

	*result = snapshot

	out, err := attempt()
	if err != nil {
		return out, fmt.Errorf("attempt: %w", err)
	}

	return out, nil
}

func isDeadlockDetected(err error) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == pgErrCodeDeadlockDetected
	}

	return false
}
