package sourceobservation

import (
	"context"
	"fmt"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"
)

func (r *Repository) measureRetentionBacklogs(ctx context.Context, cfg RetentionConfig, now time.Time, result *RetentionResult) error {
	kinds, cutoffs := evidencePolicies(cfg.EvidenceAgeByKind, now)
	if len(kinds) == 0 {
		return nil
	}

	var (
		oldest *time.Time
		known  bool
	)

	if err := pgxdb.QueryRow(ctx, r.pool, repositoryRetentionBacklogEvidenceSQL, kinds, cutoffs, MaxRetentionBatchSize).Scan(&oldest, &known); err != nil {
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
