package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

const MaxRetentionBatchSize = 1000

// sourceRetentionMaxWindows는 원본 정리가 한 tick에 처리하는 창 수의 상한이다. 운영 창 하나는 평균 약 0.7초
// (2026-10-25 pg_stat_statements, 2026-08-23 이후 29,269회)라 보통 아래 시간 예산이 먼저 멈추고, 창이 빠를 때도
// 한 tick의 원본 삭제를 창 상한 × 배치 크기로 제한한다.
const sourceRetentionMaxWindows = 8

type RetentionConfig struct {
	QueueProcessedAge    time.Duration
	QueueDLQAge          time.Duration
	EvidenceAgeByKind    map[contract.ObservationKind]time.Duration
	CollisionAge         time.Duration
	LiveAbsenceSlotAge   time.Duration
	CheckpointHistoryAge time.Duration
	BatchSize            int
}

// RetentionResult는 앞 단계의 확정된 삭제량과 실패 대상을 함께 반환한다.
// BacklogKnown이 참일 때만 BacklogAge가 유효하며, 0은 남은 삭제 후보가 없다는 뜻이다.
// Scanned와 Traversals는 원본(source_observations) 단계만 채운다. Scanned는 커서가 지나간 후보 수로 삭제분을
// 포함하므로 Scanned - Deleted가 보호·잠금으로 남긴 수이고, Traversals는 이 tick에 끝난 전체 순회 수다.
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
		c.LiveAbsenceSlotAge < 0 || c.CheckpointHistoryAge < 0
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
	out, err := r.deleteRetentionBatches(r.retentionSteps(ctx, cfg, now))
	if err != nil {
		return out, fmt.Errorf("delete retention batches: %w", err)
	}

	if err := r.measureRetentionBacklogs(ctx, cfg, now, &out); err != nil {
		return out, fmt.Errorf("measure retention backlogs: %w", err)
	}

	return out, nil
}

// RetentionTables는 RunRetentionTick이 ByTable·FailedTable로 보고하는 테이블 이름을 실행 순서대로 돌려준다.
// 지표 소비자가 같은 목록을 따로 적지 않도록 실행 단계 정의에서 이름만 읽으며, 단계 함수는 실행하지 않는다.
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
	return []retentionStep{
		{
			table: "source_observation_queue",
			age:   minPositiveDuration(cfg.QueueProcessedAge, cfg.QueueDLQAge),
			run:   func() (RetentionResult, error) { return countOnly(r.deleteQueueBatch(ctx, cfg, now)) },
		},
		{
			table: "source_observation_collisions",
			age:   cfg.CollisionAge,
			run: func() (RetentionResult, error) {
				return countOnly(r.deleteAgedBatch(ctx, "repository_retention_delete_collisions_0077_77.sql", cfg.CollisionAge, now, cfg.BatchSize))
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
				return countOnly(r.deleteAgedBatch(ctx, "repository_retention_delete_live_absence_slots.sql", cfg.LiveAbsenceSlotAge, now, cfg.BatchSize))
			},
		},
		{
			table: "source_collection_checkpoints",
			age:   cfg.CheckpointHistoryAge,
			run: func() (RetentionResult, error) {
				return countOnly(r.deleteAgedBatch(
					ctx,
					"repository_retention_delete_checkpoints_0084_84.sql",
					cfg.CheckpointHistoryAge,
					now,
					cfg.BatchSize,
				))
			},
		},
	}
}

type retentionStep struct {
	table string
	age   time.Duration
	run   func() (RetentionResult, error)
}

// countOnly는 삭제 수만 보고하는 단계의 결과를 RetentionResult로 옮긴다.
func countOnly(deleted int64, err error) (RetentionResult, error) {
	return RetentionResult{Deleted: deleted}, err
}

func (r *Repository) deleteRetentionBatches(steps []retentionStep) (RetentionResult, error) {
	var combined RetentionResult

	for _, step := range steps {
		part, err := step.run()
		// 원본 단계는 실패한 창 앞의 창을 이미 커밋했으므로 그 계수를 오류와 함께 보고한다.
		if step.age > 0 && (err == nil || part.Deleted > 0 || part.Scanned > 0) {
			part.Table = step.table

			combined.Deleted += part.Deleted

			combined.ByTable = append(combined.ByTable, part)

			if combined.Table == "" && part.Deleted > 0 {
				combined.Table = step.table
			}
		}

		if err != nil {
			// 앞 단계는 이미 별도로 커밋됐으므로 뒤 단계 오류에도 삭제량을 보존한다.
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

// deleteEvidenceWindows는 tick 시작의 cutoff로 원본 창을 이어서 처리한다. 삭제 0건이어도 커서가 전진하면 다음
// 창으로 넘어가 보호된 앞쪽 때문에 삭제 가능한 뒤쪽이 밀리지 않게 하고, 순회 완료·진척 없음·창 상한·시간 예산·
// 오류에서 멈춘다. 순회 완료에서 멈추므로 한 tick이 같은 보호 순회를 다시 돌지 않는다.
func (r *Repository) deleteEvidenceWindows(ctx context.Context, cfg RetentionConfig, now time.Time) (RetentionResult, error) {
	kinds, cutoffs := evidencePolicies(cfg.EvidenceAgeByKind, now)
	if len(kinds) == 0 {
		return RetentionResult{}, nil
	}

	// 후속 payload·absence·checkpoint 정리와 적체 측정에 남은 시한의 절반을 남긴다. 첫 창은 이전처럼 항상 실행하고,
	// 다음 창은 이번 tick에서 가장 느렸던 창만큼 걸려도 이 예산 안에 끝날 때만 시작한다.
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
			// 앞 창은 이미 커밋됐으므로 계수를 보존하고, 실패한 창은 다시 실행하지 않는다.
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
	// DB 함수가 exclusive live 전역 잠금 → 커서 → 원본 순서를 소유하며 삭제와 진행을 함께 커밋한다.
	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (evidenceWindow, error) {
		var window evidenceWindow

		if err := tx.QueryRow(ctx, mustSQL("repository_retention_delete_evidence_window.sql"), kinds, cutoffs, limit).
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

// 대부분의 사전 항목이 사용 중이어도 커서가 스캔 범위를 제한한다.
// 외래 키·행 잠금·발행자의 KEY SHARE로 조회와 INSERT 사이에 페이로드가 삭제되지 않게 한다.
func (r *Repository) deleteUnreferencedPayloadBatch(ctx context.Context, cfg RetentionConfig, now time.Time) (int64, error) {
	if minEvidenceAge(cfg.EvidenceAgeByKind) == 0 {
		return 0, nil
	}

	var removed int64

	if err := r.pool.QueryRow(
		ctx,
		mustSQL("repository_retention_delete_payloads.sql"),
		now.Add(-time.Hour),
		cfg.BatchSize,
	).Scan(&removed); err != nil {
		return 0, fmt.Errorf("collect unreferenced source observation payloads: %w", err)
	}

	return removed, nil
}

func (r *Repository) execDelete(ctx context.Context, name string, args ...any) (int64, error) {
	tag, err := r.pool.Exec(ctx, mustSQL(name), args...)
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
