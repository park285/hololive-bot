package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-api/internal/planes/youtube/targetprojection"
)

// 세대별 자식 삭제를 여러 번 진행하되 한 tick의 쓰기와 전체 DB 시한을 제한한다.
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
