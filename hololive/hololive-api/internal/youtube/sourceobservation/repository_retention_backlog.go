package sourceobservation

import (
	"context"
	"fmt"
	"time"
)

// 보호 중인 원본이 조회 한도를 채우면 적체 없음으로 단정하지 않는다.
// 감사 행은 원본과 함께 삭제되므로 별도 적체나 보존 정책이 없다.
func (r *Repository) measureRetentionBacklogs(ctx context.Context, cfg RetentionConfig, now time.Time, result *RetentionResult) error {
	kinds, cutoffs := evidencePolicies(cfg.EvidenceAgeByKind, now)
	if len(kinds) == 0 {
		return nil
	}

	var (
		oldest *time.Time
		known  bool
	)

	if err := r.pool.QueryRow(ctx, mustSQL("repository_retention_backlog_evidence.sql"), kinds, cutoffs, MaxRetentionBatchSize).Scan(&oldest, &known); err != nil {
		result.FailedTable = "source_observations"
		return fmt.Errorf("read retention backlog: source_observations: %w", err)
	}

	for i := range result.ByTable {
		part := &result.ByTable[i]
		if part.Table != "source_observations" {
			continue
		}

		part.BacklogKnown = known
		if known && oldest != nil {
			part.BacklogAge = now.Sub(*oldest)
		}
	}

	return nil
}
