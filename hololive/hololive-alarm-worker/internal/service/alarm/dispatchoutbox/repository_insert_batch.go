package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// InsertBatch는 정상 entry와 collision audit을 함께 commit한다. 충돌 entry는 delivery를 만들지 않으며
// 호출자는 error가 nil이어도 입력별 Receipts의 수용 여부를 확인해야 한다.
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

// 40P01은 tx 전체가 롤백되므로 재시도 단위도 tx 전체여야 한다 — preflight의 ORDER BY로
// publisher 교차 잠금은 제거했지만 후속 INSERT ... ON CONFLICT 경합의 40P01은 남는다.
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
