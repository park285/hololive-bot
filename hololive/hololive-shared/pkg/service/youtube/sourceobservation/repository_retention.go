package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const MaxRetentionBatchSize = 1000

type RetentionConfig struct {
	QueueProcessedAge     time.Duration
	QueueDLQAge           time.Duration
	EvidenceAgeByKind     map[contract.ObservationKind]time.Duration
	CollisionAge          time.Duration
	ReplayAuditAge        time.Duration
	ApplicationAuditGrace time.Duration
	CheckpointHistoryAge  time.Duration
	BatchSize             int
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

	if c.hasNegativeAge() {
		return errors.New("retention ages must not be negative")
	}

	if err := validateEvidenceAges(c.EvidenceAgeByKind); err != nil {
		return fmt.Errorf("validate evidence ages: %w", err)
	}

	for _, age := range c.EvidenceAgeByKind {
		if retentionApplicationAgeOverflows(age, c.ApplicationAuditGrace) {
			return errors.New("retention application audit age overflows")
		}
	}

	return nil
}

func (c RetentionConfig) hasNegativeAge() bool {
	return c.QueueProcessedAge < 0 || c.QueueDLQAge < 0 || c.CollisionAge < 0 || c.ReplayAuditAge < 0 ||
		c.ApplicationAuditGrace < 0 || c.CheckpointHistoryAge < 0
}

func retentionApplicationAgeOverflows(age, grace time.Duration) bool {
	return age > 0 && grace > 0 && age > time.Duration(1<<63-1)-grace
}

func validateEvidenceAges(ages map[contract.ObservationKind]time.Duration) error {
	for kind, age := range ages {
		if !kind.Valid() {
			return fmt.Errorf("retention evidence kind %q is invalid", kind)
		}

		if age < 0 {
			return errors.New("retention evidence age must not be negative")
		}
	}

	return nil
}

func (r *Repository) ListRetentionCandidates(
	ctx context.Context,
	query RetentionQuery,
) ([]int64, error) {
	if err := r.validate(); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}

	if !query.Kind.Valid() || query.Before.IsZero() || query.Limit < 1 || query.Limit > MaxRetentionBatchSize {
		return nil, errors.New("list source observation retention candidates: invalid query")
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

	out, err := scanRetentionIDs(rows, query.Limit)
	if err != nil {
		return out, fmt.Errorf("scan retention IDs: %w", err)
	}

	return out, nil
}

func scanRetentionIDs(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}, limit int,
) ([]int64, error) {
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
		return RetentionResult{}, fmt.Errorf("validate: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return RetentionResult{}, fmt.Errorf("run source observation retention: %w", err)
	}

	if now.IsZero() {
		return RetentionResult{}, errors.New("run source observation retention: now is required")
	}

	out, err := r.runRetentionSteps(ctx, cfg, now)
	if err != nil {
		return out, fmt.Errorf("run retention steps: %w", err)
	}

	return out, nil
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
		{
			table: "source_observation_applications",
			age:   minApplicationAuditAge(cfg),
			run:   func() (int64, error) { return r.deleteApplicationBatch(ctx, cfg, now) },
		},
		{
			table: "source_collection_checkpoints",
			age:   cfg.CheckpointHistoryAge,
			run: func() (int64, error) {
				return r.deleteAgedBatch(
					ctx,
					"repository_retention_delete_checkpoints_0084_84.sql",
					cfg.CheckpointHistoryAge,
					now,
					cfg.BatchSize,
				)
			},
		},
	}

	out, err := r.deleteFirstRetentionBatch(cfg.BatchSize, steps)
	if err != nil {
		return out, fmt.Errorf("delete first retention batch: %w", err)
	}

	return out, nil
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

	out, err := r.execDelete(
		ctx,
		"repository_retention_delete_queue_0076_76.sql",
		processedBefore,
		dlqBefore,
		cfg.BatchSize,
	)
	if err != nil {
		return out, fmt.Errorf("exec delete: %w", err)
	}

	return out, nil
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

	out, err := r.execDelete(ctx, sqlName, cutoff, limit)
	if err != nil {
		return out, fmt.Errorf("exec delete: %w", err)
	}

	return out, nil
}

func (r *Repository) deleteEvidenceBatch(ctx context.Context, cfg RetentionConfig, now time.Time) (int64, error) {
	kinds, cutoffs := evidencePolicies(cfg.EvidenceAgeByKind, now)
	if len(kinds) == 0 {
		return 0, nil
	}

	out, err := r.execDelete(ctx, "repository_retention_delete_evidence_0079_79.sql", kinds, cutoffs, cfg.BatchSize)
	if err != nil {
		return out, fmt.Errorf("exec delete: %w", err)
	}

	return out, nil
}

func (r *Repository) deleteApplicationBatch(ctx context.Context, cfg RetentionConfig, now time.Time) (int64, error) {
	kinds, cutoffs := applicationAuditPolicies(cfg.EvidenceAgeByKind, cfg.ApplicationAuditGrace, now)
	if len(kinds) == 0 {
		return 0, nil
	}

	out, err := r.execDelete(ctx, "repository_retention_delete_applications_0083_83.sql", kinds, cutoffs, cfg.BatchSize)
	if err != nil {
		return out, fmt.Errorf("exec delete: %w", err)
	}

	return out, nil
}

func (r *Repository) execDelete(ctx context.Context, name string, args ...any) (int64, error) {
	tag, err := r.pool.Exec(ctx, mustSQL(name), args...)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	return tag.RowsAffected(), nil
}
