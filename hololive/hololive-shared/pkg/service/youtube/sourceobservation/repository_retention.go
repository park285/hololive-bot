package sourceobservation

import (
	"context"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const MaxRetentionBatchSize = 1000

type RetentionConfig struct {
	QueueProcessedAge time.Duration
	QueueDLQAge       time.Duration
	EvidenceAgeByKind map[contract.ObservationKind]time.Duration
	CollisionAge      time.Duration
	ReplayAuditAge    time.Duration
	BatchSize         int
}

type RetentionResult struct {
	Table      string
	Deleted    int64
	BacklogAge time.Duration
	ByTable    []RetentionResult
}

func (c RetentionConfig) Validate() error {
	if c.BatchSize < 1 || c.BatchSize > MaxRetentionBatchSize {
		return fmt.Errorf("retention batch size must be between 1 and %d", MaxRetentionBatchSize)
	}
	if c.QueueProcessedAge < 0 || c.QueueDLQAge < 0 || c.CollisionAge < 0 || c.ReplayAuditAge < 0 {
		return fmt.Errorf("retention ages must not be negative")
	}
	return validateEvidenceAges(c.EvidenceAgeByKind)
}

func validateEvidenceAges(ages map[contract.ObservationKind]time.Duration) error {
	for kind, age := range ages {
		if !kind.Valid() {
			return fmt.Errorf("retention evidence kind %q is invalid", kind)
		}
		if age < 0 {
			return fmt.Errorf("retention evidence age must not be negative")
		}
	}
	return nil
}

func (r *Repository) ListRetentionCandidates(
	ctx context.Context,
	query RetentionQuery,
) ([]int64, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	if !query.Kind.Valid() || query.Before.IsZero() || query.Limit < 1 || query.Limit > MaxRetentionBatchSize {
		return nil, fmt.Errorf("list source observation retention candidates: invalid query")
	}
	rows, err := r.pool.Query(
		ctx,
		mustSQL("repository_retention_candidates_0027_27.sql"),
		query.Kind,
		query.Before,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list source observation retention candidates: %w", err)
	}
	defer rows.Close()
	return scanRetentionIDs(rows, query.Limit)
}

func scanRetentionIDs(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}, limit int) ([]int64, error) {
	result := make([]int64, 0, limit)
	for rows.Next() {
		var observationID int64
		if err := rows.Scan(&observationID); err != nil {
			return nil, fmt.Errorf("list source observation retention candidates: scan: %w", err)
		}
		result = append(result, observationID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list source observation retention candidates: iterate: %w", err)
	}
	return result, nil
}

func (r *Repository) RunRetentionTick(
	ctx context.Context,
	cfg RetentionConfig,
	now time.Time,
) (RetentionResult, error) {
	if err := r.validate(); err != nil {
		return RetentionResult{}, err
	}
	if err := cfg.Validate(); err != nil {
		return RetentionResult{}, fmt.Errorf("run source observation retention: %w", err)
	}
	if now.IsZero() {
		return RetentionResult{}, fmt.Errorf("run source observation retention: now is required")
	}
	return r.runRetentionSteps(ctx, cfg, now)
}

func (r *Repository) runRetentionSteps(
	ctx context.Context,
	cfg RetentionConfig,
	now time.Time,
) (RetentionResult, error) {
	steps := []retentionStep{
		{
			table: "source_observation_queue",
			age:   minPositiveDuration(cfg.QueueProcessedAge, cfg.QueueDLQAge),
			run:   func() (int64, error) { return r.deleteQueueBatch(ctx, cfg, now) },
		},
		{
			table: "source_observation_collisions",
			age:   cfg.CollisionAge,
			run: func() (int64, error) {
				return r.deleteAgedBatch(ctx, "repository_retention_delete_collisions_0077_77.sql", cfg.CollisionAge, now, cfg.BatchSize)
			},
		},
		{
			table: "source_observation_replay_requests",
			age:   cfg.ReplayAuditAge,
			run: func() (int64, error) {
				return r.deleteAgedBatch(ctx, "repository_retention_delete_replay_0078_78.sql", cfg.ReplayAuditAge, now, cfg.BatchSize)
			},
		},
		{
			table: "source_observations",
			age:   minEvidenceAge(cfg.EvidenceAgeByKind),
			run:   func() (int64, error) { return r.deleteEvidenceBatch(ctx, cfg, now) },
		},
	}
	return r.deleteFirstRetentionBatch(cfg.BatchSize, steps)
}

type retentionStep struct {
	table string
	age   time.Duration
	run   func() (int64, error)
}

func (r *Repository) deleteFirstRetentionBatch(_ int, steps []retentionStep) (RetentionResult, error) {
	var combined RetentionResult
	for _, step := range steps {
		deleted, err := step.run()
		if err != nil {
			return RetentionResult{Table: step.table}, fmt.Errorf("run source observation retention: %s: %w", step.table, err)
		}
		if deleted == 0 {
			continue
		}
		combined.Deleted += deleted
		combined.ByTable = append(combined.ByTable, RetentionResult{Table: step.table, Deleted: deleted})
		if combined.Table == "" {
			combined.Table = step.table
		}
	}
	return combined, nil
}

func (r *Repository) deleteQueueBatch(ctx context.Context, cfg RetentionConfig, now time.Time) (int64, error) {
	processedBefore, okProcessed := cutoffOrNil(now, cfg.QueueProcessedAge)
	dlqBefore, okDLQ := cutoffOrNil(now, cfg.QueueDLQAge)
	if !okProcessed && !okDLQ {
		return 0, nil
	}
	return r.execDelete(
		ctx,
		"repository_retention_delete_queue_0076_76.sql",
		processedBefore,
		dlqBefore,
		cfg.BatchSize,
	)
}

func (r *Repository) deleteAgedBatch(
	ctx context.Context,
	sqlName string,
	age time.Duration,
	now time.Time,
	limit int,
) (int64, error) {
	cutoff, ok := cutoffOrNil(now, age)
	if !ok {
		return 0, nil
	}
	return r.execDelete(ctx, sqlName, cutoff, limit)
}

func (r *Repository) deleteEvidenceBatch(ctx context.Context, cfg RetentionConfig, now time.Time) (int64, error) {
	kinds, cutoffs := evidencePolicies(cfg.EvidenceAgeByKind, now)
	if len(kinds) == 0 {
		return 0, nil
	}
	return r.execDelete(ctx, "repository_retention_delete_evidence_0079_79.sql", kinds, cutoffs, cfg.BatchSize)
}

func (r *Repository) execDelete(ctx context.Context, name string, args ...any) (int64, error) {
	tag, err := r.pool.Exec(ctx, mustSQL(name), args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func cutoffOrNil(now time.Time, age time.Duration) (any, bool) {
	if age <= 0 {
		return nil, false
	}
	return now.Add(-age), true
}

func evidencePolicies(ages map[contract.ObservationKind]time.Duration, now time.Time) ([]string, []time.Time) {
	kinds := make([]string, 0, len(ages))
	cutoffs := make([]time.Time, 0, len(ages))
	for kind, age := range ages {
		if age <= 0 || !kind.Valid() {
			continue
		}
		kinds = append(kinds, string(kind))
		cutoffs = append(cutoffs, now.Add(-age))
	}
	return kinds, cutoffs
}

func minPositiveDuration(values ...time.Duration) time.Duration {
	var min time.Duration
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if min == 0 || value < min {
			min = value
		}
	}
	return min
}

func minEvidenceAge(ages map[contract.ObservationKind]time.Duration) time.Duration {
	var min time.Duration
	for _, age := range ages {
		if age <= 0 {
			continue
		}
		if min == 0 || age < min {
			min = age
		}
	}
	return min
}
