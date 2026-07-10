package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *PgxRepository) InsertPending(ctx context.Context, envelope *domain.AlarmQueueEnvelope) (*Record, InsertResult, error) {
	result, err := r.InsertBatch(ctx, PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{*envelope}, Status: StatusPending})
	if err != nil {
		return nil, "", err
	}
	record, err := r.findByDedupeKey(ctx, BuildDedupeKeyFromEnvelope(envelope))
	if err != nil {
		return nil, "", err
	}
	if result.InsertedDeliveries > 0 {
		return record, Inserted, nil
	}
	return record, insertDuplicateResult(record.Status), nil
}

func insertDuplicateResult(status Status) InsertResult {
	terminal := map[Status]struct{}{
		StatusSent:        {},
		StatusDLQ:         {},
		StatusQuarantined: {},
		StatusCancelled:   {},
	}
	if _, ok := terminal[status]; ok {
		return DuplicateTerminal
	}

	switch status {
	case StatusPending, StatusLeased, StatusRetry, StatusSending, StatusSent, StatusDLQ, StatusQuarantined, StatusCancelled:
		return DuplicateActive
	default:
		return DuplicateActive
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
	if status != StatusPending {
		return PublishBatchResult{}, fmt.Errorf("insert dispatch ledger batch: unsupported status %q", status)
	}
	result := PublishBatchResult{RequestedDeliveries: len(input.Envelopes)}
	if len(input.Envelopes) == 0 {
		return result, nil
	}

	eventRows, deliveries, preflightCollisions, err := prepareInsertBatchRows(input.Envelopes, status, &result)
	if err != nil {
		return result, err
	}
	return runPublishBatchWithDeadlockRetry(&result, func() (PublishBatchResult, error) {
		return r.insertPreparedBatch(ctx, eventRows, deliveries, preflightCollisions, &result)
	})
}

const pgErrCodeDeadlockDetected = "40P01"

// 40P01은 tx 전체가 롤백되므로 재시도 단위도 tx 전체여야 한다 — preflight의 ORDER BY로
// publisher 교차 잠금은 제거했지만 후속 INSERT ... ON CONFLICT 경합의 40P01은 남는다.
func runPublishBatchWithDeadlockRetry(
	result *PublishBatchResult,
	attempt func() (PublishBatchResult, error),
) (PublishBatchResult, error) {
	snapshot := *result
	publishResult, err := attempt()
	if !isDeadlockDetected(err) {
		return publishResult, err
	}
	*result = snapshot
	return attempt()
}

func isDeadlockDetected(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgErrCodeDeadlockDetected
	}
	return false
}
