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

	out, err := r.recoverWithQuery(ctx, mustSQL("repository_maintenance_0010_01.sql"), limit, maxDeliveriesPerSendUnit)
	if err != nil {
		return out, fmt.Errorf("recover with query: %w", err)
	}

	return out, nil
}

func (r *PgxRepository) QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	seconds := int(olderThan.Seconds())
	if seconds <= 0 {
		seconds = 300
	}

	out, err := r.recoverWithQuery(ctx, mustSQL("repository_maintenance_0035_02.sql"), limit, seconds)
	if err != nil {
		return out, fmt.Errorf("recover with query: %w", err)
	}

	return out, nil
}

func (r *PgxRepository) recoverWithQuery(ctx context.Context, query string, args ...any) (int, error) {
	if len(args) == 0 {
		args = append(args, 100)
	}

	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("recover dispatch deliveries: %w", err)
	}

	return int(tag.RowsAffected()), nil
}
