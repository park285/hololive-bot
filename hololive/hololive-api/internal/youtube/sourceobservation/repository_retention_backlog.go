package sourceobservation

import (
	"context"
	"fmt"
	"time"
)

// 삭제 뒤 큰 두 테이블의 인덱스 선두만 확인한다. 보호 중인 원본이 조회 한도를
// 채우면 적체 없음으로 단정하지 않고 미확정으로 남긴다. 삭제 정책과 배치는 바꾸지 않는다.
func (r *Repository) measureRetentionBacklogs(ctx context.Context, cfg RetentionConfig, now time.Time, result *RetentionResult) error {
	evidenceKinds, evidenceCutoffs := evidencePolicies(cfg.EvidenceAgeByKind, now)
	applicationKinds, applicationCutoffs := applicationAuditPolicies(cfg.EvidenceAgeByKind, cfg.ApplicationAuditGrace, now)

	for _, query := range []struct {
		table   string
		sql     string
		kinds   []string
		cutoffs []time.Time
		limit   int
	}{
		{"source_observations", "repository_retention_backlog_evidence.sql", evidenceKinds, evidenceCutoffs, MaxRetentionBatchSize},
		{"source_observation_applications", "repository_retention_backlog_applications.sql", applicationKinds, applicationCutoffs, 1},
	} {
		if len(query.kinds) == 0 {
			continue
		}

		var (
			oldest *time.Time
			known  bool
		)

		if err := r.pool.QueryRow(ctx, mustSQL(query.sql), query.kinds, query.cutoffs, query.limit).Scan(&oldest, &known); err != nil {
			result.FailedTable = query.table

			return fmt.Errorf("read retention backlog: %s: %w", query.table, err)
		}

		for i := range result.ByTable {
			part := &result.ByTable[i]
			if part.Table != query.table {
				continue
			}

			part.BacklogKnown = known

			if known && oldest != nil {
				part.BacklogAge = now.Sub(*oldest)
			}
		}
	}

	return nil
}
