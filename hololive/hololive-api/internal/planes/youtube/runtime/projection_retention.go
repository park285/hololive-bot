package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
)

// projectionRetentionMaxBatches는 실측 약 12세대/120초 유입을 처리하면서도
// 한 tick의 쓰기를 제한한다. 각 배치의 1,000행 상한과 전체 DB 시한은 유지한다.
const projectionRetentionMaxBatches = 64

func (r *Runtime) retainProjectionBatches(ctx context.Context, now time.Time) (targetprojection.RetentionResult, error) {
	var total targetprojection.RetentionResult

	for range projectionRetentionMaxBatches {
		if err := ctx.Err(); err != nil {
			return total, fmt.Errorf("projection retention budget: %w", err)
		}

		part, err := r.projectionRetainer.Retain(ctx, now, r.Config.Retention.ProjectionRetiredAge, r.Config.Retention.BatchSize)
		// Retain은 lease 삭제와 자식 배치를 따로 commit한다. 뒤 배치가 실패해도
		// 이미 확정된 삭제 계수는 보존하며, 실패한 문장을 다시 실행하지 않는다.
		total.LeasesDeleted += part.LeasesDeleted
		total.ReasonsDeleted += part.ReasonsDeleted
		total.TargetsDeleted += part.TargetsDeleted
		total.GenerationsDeleted += part.GenerationsDeleted

		if err != nil {
			return total, fmt.Errorf("retain projection batch: %w", err)
		}

		// 한 generation의 마지막 배치는 상한보다 작아도 다음 세대가 남는다.
		if part == (targetprojection.RetentionResult{}) {
			return total, nil
		}
	}

	return total, nil
}
