package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RecoverExpiredLeased는 발송 단위를 분할하지 않는다. 첫 단위가 limit보다 크면 그 단위 전체를
// 복구하므로 반환 행 수는 최대 maxDeliveriesPerSendUnit-1만큼 상한을 넘을 수 있다.
func (r *PgxRepository) RecoverExpiredLeased(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("recover expired leased dispatch deliveries: limit must be positive")
	}

	tag, err := r.pool.Exec(ctx, mustSQL("repository_maintenance_0010_01.sql"), limit, maxDeliveriesPerSendUnit)
	if err != nil {
		return 0, fmt.Errorf("recover expired leased dispatch deliveries: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

// QuarantineStaleSending은 sending_started_at이 olderThan보다 오래된 sending을 결과 불명으로 격리한다.
func (r *PgxRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	olderThanMS, err := leaseMilliseconds(olderThan)
	if err != nil {
		return 0, fmt.Errorf("quarantine stale sending dispatch deliveries: older than: %w", err)
	}

	tag, err := r.pool.Exec(ctx, mustSQL("repository_maintenance_0035_02.sql"), limit, olderThanMS)
	if err != nil {
		return 0, fmt.Errorf("quarantine stale sending dispatch deliveries: %w", err)
	}

	return int(tag.RowsAffected()), nil
}
