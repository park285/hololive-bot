package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

const MaxRetentionBatchSize = 1000

const sourceRetentionMaxWindows = 8

type RetentionConfig struct {
	QueueProcessedAge     time.Duration
	QueueDLQAge           time.Duration
	EvidenceAgeByKind     map[contract.ObservationKind]time.Duration
	CollisionAge          time.Duration
	LiveAbsenceSlotAge    time.Duration
	ContentAbsenceSlotAge time.Duration
	CheckpointHistoryAge  time.Duration
	BatchSize             int
}

type RetentionResult struct {
	Table        string
	FailedTable  string
	Deleted      int64
	Scanned      int64
	Traversals   int64
	BacklogAge   time.Duration
	BacklogKnown bool
	ByTable      []RetentionResult
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

	return nil
}

func (c RetentionConfig) hasNegativeAge() bool {
	return c.QueueProcessedAge < 0 || c.QueueDLQAge < 0 || c.CollisionAge < 0 ||
		c.LiveAbsenceSlotAge < 0 || c.ContentAbsenceSlotAge < 0 || c.CheckpointHistoryAge < 0
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
	out, err := r.deleteRetentionBatches(r.retentionSteps(ctx, cfg, now))
	if err != nil {
		return out, fmt.Errorf("delete retention batches: %w", err)
	}

	if err := r.measureRetentionBacklogs(ctx, cfg, now, &out); err != nil {
		return out, fmt.Errorf("measure retention backlogs: %w", err)
	}

	return out, nil
}

func RetentionTables() []string {
	steps := (*Repository)(nil).retentionSteps(context.Background(), RetentionConfig{}, time.Time{})
	tables := make([]string, 0, len(steps))

	for _, step := range steps {
		tables = append(tables, step.table)
	}

	return tables
}

func (r *Repository) retentionSteps(
	ctx context.Context,
	cfg RetentionConfig,
	now time.Time,
) []retentionStep {
	return append([]retentionStep{
		{
			table: "source_observation_queue",
			age:   minPositiveDuration(cfg.QueueProcessedAge, cfg.QueueDLQAge),
			run:   func() (RetentionResult, error) { return countOnly(r.deleteQueueBatch(ctx, cfg, now)) },
		},
		{
			table: "source_observation_collisions",
			age:   cfg.CollisionAge,
			run: func() (RetentionResult, error) {
				return countOnly(r.deleteAgedBatch(ctx, retentionStepsSQL, cfg.CollisionAge, now, cfg.BatchSize))
			},
		},
		{
			table: "source_observations",
			age:   minEvidenceAge(cfg.EvidenceAgeByKind),
			run:   func() (RetentionResult, error) { return r.deleteEvidenceWindows(ctx, cfg, now) },
		},
		{
			table: "source_observation_payloads",
			age:   minEvidenceAge(cfg.EvidenceAgeByKind),
			run:   func() (RetentionResult, error) { return countOnly(r.deleteUnreferencedPayloadBatch(ctx, cfg, now)) },
		},
		{
			table: "youtube_live_absence_slots",
			age:   cfg.LiveAbsenceSlotAge,
			run: func() (RetentionResult, error) {
				return countOnly(r.deleteAgedBatch(ctx, repositoryRetentionDeleteLiveAbsenceSlotsSQL, cfg.LiveAbsenceSlotAge, now, cfg.BatchSize))
			},
		},
		{
			table: "source_collection_checkpoints",
			age:   cfg.CheckpointHistoryAge,
			run: func() (RetentionResult, error) {
				return countOnly(r.deleteAgedBatch(
					ctx,
					retentionSteps2SQL,
					cfg.CheckpointHistoryAge,
					now,
					cfg.BatchSize,
				))
			},
		},
	}, r.auditRetentionSteps(ctx, cfg, now)...)
}

func (r *Repository) auditRetentionSteps(
	ctx context.Context,
	cfg RetentionConfig,
	now time.Time,
) []retentionStep {
	profileAge := cfg.EvidenceAgeByKind[contract.KindChannelProfile]
	photoAge := cfg.EvidenceAgeByKind[contract.KindChannelPhoto]

	return []retentionStep{
		{
			table: "youtube_content_absence_slots",
			age:   cfg.ContentAbsenceSlotAge,
			run: func() (RetentionResult, error) {
				return countOnly(r.deleteAgedBatch(ctx, repositoryRetentionDeleteContentAbsenceSlotsSQL, cfg.ContentAbsenceSlotAge, now, cfg.BatchSize))
			},
		},
		{
			table: "youtube_channel_profile_evidence",
			age:   profileAge,
			run: func() (RetentionResult, error) {
				return countOnly(r.deleteAgedBatch(ctx, repositoryRetentionDeleteProfileEvidenceSQL, profileAge, now, cfg.BatchSize))
			},
		},
		{
			table: "youtube_channel_photo_variants",
			age:   photoAge,
			run: func() (RetentionResult, error) {
				return countOnly(r.deleteAgedBatch(ctx, repositoryRetentionDeletePhotoVariantsSQL, photoAge, now, cfg.BatchSize))
			},
		},
		{
			table: "source_observation_provenances",
			age:   minEvidenceAge(cfg.EvidenceAgeByKind),
			run:   func() (RetentionResult, error) { return countOnly(r.deleteUnreferencedProvenanceBatch(ctx, cfg)) },
		},
	}
}

type retentionStep struct {
	table string
	age   time.Duration
	run   func() (RetentionResult, error)
}

func countOnly(deleted int64, err error) (RetentionResult, error) {
	return RetentionResult{Deleted: deleted}, err
}

func (r *Repository) deleteRetentionBatches(steps []retentionStep) (RetentionResult, error) {
	var combined RetentionResult

	for _, step := range steps {
		part, err := step.run()
		if step.age > 0 && (err == nil || part.Deleted > 0 || part.Scanned > 0) {
			part.Table = step.table

			combined.Deleted += part.Deleted

			combined.ByTable = append(combined.ByTable, part)

			if combined.Table == "" && part.Deleted > 0 {
				combined.Table = step.table
			}
		}

		if err != nil {
			combined.FailedTable = step.table

			return combined, fmt.Errorf("run source observation retention: %s: %w", step.table, err)
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
		deleteQueueBatchSQL,
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
	query pgxdb.Statement,
	age time.Duration,
	now time.Time,
	limit int,
) (int64, error) {
	cutoff, ok := cutoffOrNil(now, age)
	if !ok {
		return 0, nil
	}

	out, err := r.execDelete(ctx, query, cutoff, limit)
	if err != nil {
		return out, fmt.Errorf("exec delete: %w", err)
	}

	return out, nil
}

func (r *Repository) deleteEvidenceWindows(ctx context.Context, cfg RetentionConfig, now time.Time) (RetentionResult, error) {
	kinds, cutoffs := evidencePolicies(cfg.EvidenceAgeByKind, now)
	if len(kinds) == 0 {
		return RetentionResult{}, nil
	}

	deadline, bounded := ctx.Deadline()
	budgetEnd := time.Now().Add(time.Until(deadline) / 2)

	var (
		out     RetentionResult
		slowest time.Duration
	)

	for window := range sourceRetentionMaxWindows {
		if window > 0 && bounded && time.Now().Add(slowest).After(budgetEnd) {
			break
		}

		started := time.Now()

		part, err := r.deleteEvidenceWindow(ctx, kinds, cutoffs, cfg.BatchSize)
		if err != nil {
			return out, err
		}

		slowest = max(slowest, time.Since(started))

		out.Deleted += part.deleted
		out.Scanned += part.passed

		if part.traversalCompleted {
			out.Traversals++

			break
		}

		if part.passed == 0 {
			break
		}
	}

	return out, nil
}

type evidenceWindow struct {
	deleted            int64
	passed             int64
	traversalCompleted bool
}

func (r *Repository) deleteEvidenceWindow(ctx context.Context, kinds []string, cutoffs []time.Time, limit int) (evidenceWindow, error) {
	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (evidenceWindow, error) {
		var window evidenceWindow

		if err := pgxdb.QueryRow(ctx, tx, repositoryRetentionDeleteEvidenceWindowSQL, kinds, cutoffs, limit).
			Scan(&window.deleted, &window.passed, &window.traversalCompleted); err != nil {
			return evidenceWindow{}, fmt.Errorf("delete source observation evidence: %w", err)
		}

		return window, nil
	})
	if err != nil {
		return evidenceWindow{}, fmt.Errorf("delete evidence transaction: %w", err)
	}

	return out, nil
}

func (r *Repository) deleteUnreferencedPayloadBatch(ctx context.Context, cfg RetentionConfig, now time.Time) (int64, error) {
	if minEvidenceAge(cfg.EvidenceAgeByKind) == 0 {
		return 0, nil
	}

	var removed int64

	if err := pgxdb.QueryRow(ctx, r.pool,
		repositoryRetentionDeletePayloadsSQL,
		now.Add(-time.Hour),
		cfg.BatchSize,
	).Scan(&removed); err != nil {
		return 0, fmt.Errorf("collect unreferenced source observation payloads: %w", err)
	}

	return removed, nil
}

func (r *Repository) deleteUnreferencedProvenanceBatch(ctx context.Context, cfg RetentionConfig) (int64, error) {
	if minEvidenceAge(cfg.EvidenceAgeByKind) == 0 {
		return 0, nil
	}

	var removed int64

	if err := pgxdb.QueryRow(ctx, r.pool, repositoryRetentionDeleteProvenancesSQL, cfg.BatchSize).Scan(&removed); err != nil {
		return 0, fmt.Errorf("collect unreferenced source observation provenances: %w", err)
	}

	return removed, nil
}

func (r *Repository) execDelete(ctx context.Context, query pgxdb.Statement, args ...any) (int64, error) {
	tag, err := pgxdb.Exec(ctx, r.pool, query, args...)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
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
	var minimum time.Duration

	for _, value := range values {
		if value <= 0 {
			continue
		}

		if minimum == 0 || value < minimum {
			minimum = value
		}
	}

	return minimum
}

func minEvidenceAge(ages map[contract.ObservationKind]time.Duration) time.Duration {
	var minimum time.Duration

	for _, age := range ages {
		if age <= 0 {
			continue
		}

		if minimum == 0 || age < minimum {
			minimum = age
		}
	}

	return minimum
}
