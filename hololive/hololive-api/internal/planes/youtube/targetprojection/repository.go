package targetprojection

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type Result struct {
	Generation int64
	RowCount   int
	SHA256     string
	Changed    bool
}

type Refresher struct {
	pool     *pgxpool.Pool
	validity time.Duration
}

func NewRefresher(pool *pgxpool.Pool, validity time.Duration) (*Refresher, error) {
	if pool == nil {
		return nil, fmt.Errorf("create youtube target projection refresher: pool is nil")
	}
	if validity < MinValidity || validity > MaxValidity {
		return nil, fmt.Errorf("create youtube target projection refresher: validity must be between %s and %s", MinValidity, MaxValidity)
	}
	return &Refresher{pool: pool, validity: validity}, nil
}

func (r *Refresher) Refresh(ctx context.Context, builder Builder, now time.Time) (Result, error) {
	if r == nil || r.pool == nil || builder == nil || now.IsZero() {
		return Result{}, fmt.Errorf("refresh youtube target projection: invalid refresher, builder, or time")
	}
	now = now.UTC()
	validUntil := now.Add(r.validity)
	return dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (Result, error) {
		targets, reasons, err := builder.Build(ctx, tx, now)
		if err != nil {
			return Result{}, fmt.Errorf("refresh youtube target projection: build: %w", err)
		}
		targets, reasons, hash, err := normalize(targets, reasons)
		if err != nil {
			return Result{}, fmt.Errorf("refresh youtube target projection: normalize: %w", err)
		}
		return refreshTx(ctx, tx, targets, reasons, hash, now, validUntil)
	})
}

type currentProjection struct {
	generation int64
	rowCount   int
	hash       string
	found      bool
}

func refreshTx(ctx context.Context, tx dbx.Tx, targets []TargetSpec, reasons []TargetReason, hash string, now, validUntil time.Time) (Result, error) {
	current, err := lockCurrent(ctx, tx)
	if err != nil {
		return Result{}, err
	}
	if current.found && current.rowCount == len(targets) && current.hash == hash {
		if _, err := tx.Exec(ctx, `
			UPDATE youtube_collection_projection_generations
			SET valid_until = $2
			WHERE generation = $1 AND status = 'CURRENT'
		`, current.generation, validUntil); err != nil {
			return Result{}, fmt.Errorf("refresh youtube target projection: extend generation validity: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE youtube_collection_targets
			SET valid_until = $2
			WHERE projection_generation = $1
		`, current.generation, validUntil); err != nil {
			return Result{}, fmt.Errorf("refresh youtube target projection: extend target validity: %w", err)
		}
		storedReasons, err := loadReasons(ctx, tx, current.generation)
		if err != nil {
			return Result{}, err
		}
		if !sameReasons(storedReasons, reasons) {
			if _, err := tx.Exec(ctx, `DELETE FROM youtube_collection_target_reasons WHERE projection_generation = $1`, current.generation); err != nil {
				return Result{}, fmt.Errorf("refresh youtube target projection: replace reasons: %w", err)
			}
			if err := insertReasons(ctx, tx, current.generation, reasons); err != nil {
				return Result{}, err
			}
		}
		return Result{Generation: current.generation, RowCount: len(targets), SHA256: hash}, nil
	}

	var generation int64
	err = tx.QueryRow(ctx, `
		INSERT INTO youtube_collection_projection_generations (
			status, row_count, projection_sha256, valid_until
		) VALUES ('STAGING', $1, $2, $3)
		RETURNING generation
	`, len(targets), hash, validUntil).Scan(&generation)
	if err != nil {
		return Result{}, fmt.Errorf("refresh youtube target projection: insert staging generation: %w", err)
	}
	if err := insertTargets(ctx, tx, generation, validUntil, targets); err != nil {
		return Result{}, err
	}
	if err := insertReasons(ctx, tx, generation, reasons); err != nil {
		return Result{}, err
	}
	loadedTargets, err := loadTargets(ctx, tx, generation)
	if err != nil {
		return Result{}, err
	}
	loadedReasons, err := loadReasons(ctx, tx, generation)
	if err != nil {
		return Result{}, err
	}
	_, _, storedHash, err := normalize(loadedTargets, reasons)
	if err != nil || len(loadedTargets) != len(targets) || storedHash != hash || !sameReasons(loadedReasons, reasons) {
		return Result{}, fmt.Errorf("refresh youtube target projection: staging verification failed: %w", ErrInvalidProjection)
	}
	if current.found {
		if _, err := tx.Exec(ctx, `
			UPDATE youtube_collection_projection_generations
			SET status = 'RETIRED'
			WHERE generation = $1 AND status = 'CURRENT'
		`, current.generation); err != nil {
			return Result{}, fmt.Errorf("refresh youtube target projection: retire current generation: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE youtube_collection_projection_generations
		SET status = 'CURRENT', activated_at = $2
		WHERE generation = $1 AND status = 'STAGING'
	`, generation, now); err != nil {
		return Result{}, fmt.Errorf("refresh youtube target projection: activate staging generation: %w", err)
	}
	return Result{Generation: generation, RowCount: len(targets), SHA256: hash, Changed: true}, nil
}

func lockCurrent(ctx context.Context, tx dbx.Tx) (currentProjection, error) {
	var current currentProjection
	err := tx.QueryRow(ctx, `
		SELECT generation, row_count, projection_sha256
		FROM youtube_collection_projection_generations
		WHERE status = 'CURRENT'
		FOR UPDATE
	`).Scan(&current.generation, &current.rowCount, &current.hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return current, nil
	}
	if err != nil {
		return current, fmt.Errorf("refresh youtube target projection: lock current generation: %w", err)
	}
	current.found = true
	return current, nil
}

func insertTargets(ctx context.Context, tx dbx.Tx, generation int64, validUntil time.Time, targets []TargetSpec) error {
	if len(targets) == 0 {
		return nil
	}
	subjects := make([]string, len(targets))
	kinds := make([]string, len(targets))
	priorities := make([]int16, len(targets))
	intervals := make([]int64, len(targets))
	enabled := make([]bool, len(targets))
	for i := range targets {
		subjects[i] = targets[i].SubjectKey
		kinds[i] = string(targets[i].ObservationKind)
		priorities[i] = targets[i].Priority
		intervals[i] = targets[i].PollInterval.Milliseconds()
		enabled[i] = targets[i].Enabled
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO youtube_collection_targets (
			projection_generation, subject_key, observation_kind,
			priority, poll_interval_ms, enabled, valid_until
		)
		SELECT $1, input.subject_key, input.observation_kind,
		       input.priority, input.poll_interval_ms, input.enabled, $7
		FROM unnest($2::text[], $3::text[], $4::smallint[], $5::bigint[], $6::boolean[])
		     AS input(subject_key, observation_kind, priority, poll_interval_ms, enabled)
	`, generation, subjects, kinds, priorities, intervals, enabled, validUntil); err != nil {
		return fmt.Errorf("refresh youtube target projection: insert targets: %w", err)
	}
	return nil
}

func insertReasons(ctx context.Context, tx dbx.Tx, generation int64, reasons []TargetReason) error {
	if len(reasons) == 0 {
		return nil
	}
	subjects := make([]string, len(reasons))
	kinds := make([]string, len(reasons))
	reasonKinds := make([]string, len(reasons))
	reasonKeys := make([]string, len(reasons))
	for i := range reasons {
		subjects[i] = reasons[i].SubjectKey
		kinds[i] = string(reasons[i].ObservationKind)
		reasonKinds[i] = reasons[i].ReasonKind
		reasonKeys[i] = reasons[i].ReasonKey
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO youtube_collection_target_reasons (
			projection_generation, subject_key, observation_kind, reason_kind, reason_key
		)
		SELECT $1, input.subject_key, input.observation_kind, input.reason_kind, input.reason_key
		FROM unnest($2::text[], $3::text[], $4::text[], $5::text[])
		     AS input(subject_key, observation_kind, reason_kind, reason_key)
	`, generation, subjects, kinds, reasonKinds, reasonKeys); err != nil {
		return fmt.Errorf("refresh youtube target projection: insert reasons: %w", err)
	}
	return nil
}

func loadTargets(ctx context.Context, tx dbx.Tx, generation int64) ([]TargetSpec, error) {
	rows, err := tx.Query(ctx, `
		SELECT subject_key, observation_kind, priority, poll_interval_ms, enabled
		FROM youtube_collection_targets
		WHERE projection_generation = $1
		ORDER BY subject_key, observation_kind
	`, generation)
	if err != nil {
		return nil, fmt.Errorf("refresh youtube target projection: verify target rows: %w", err)
	}
	defer rows.Close()
	targets := make([]TargetSpec, 0)
	for rows.Next() {
		var target TargetSpec
		var kind string
		var intervalMS int64
		if err := rows.Scan(&target.SubjectKey, &kind, &target.Priority, &intervalMS, &target.Enabled); err != nil {
			return nil, fmt.Errorf("refresh youtube target projection: scan target row: %w", err)
		}
		target.ObservationKind = contract.ObservationKind(kind)
		target.PollInterval = time.Duration(intervalMS) * time.Millisecond
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("refresh youtube target projection: read target rows: %w", err)
	}
	return targets, nil
}

func loadReasons(ctx context.Context, tx dbx.Tx, generation int64) ([]TargetReason, error) {
	rows, err := tx.Query(ctx, `
		SELECT subject_key, observation_kind, reason_kind, reason_key
		FROM youtube_collection_target_reasons
		WHERE projection_generation = $1
		ORDER BY subject_key, observation_kind, reason_kind, reason_key
	`, generation)
	if err != nil {
		return nil, fmt.Errorf("refresh youtube target projection: load reasons: %w", err)
	}
	defer rows.Close()
	reasons := make([]TargetReason, 0)
	for rows.Next() {
		var reason TargetReason
		var kind string
		if err := rows.Scan(&reason.SubjectKey, &kind, &reason.ReasonKind, &reason.ReasonKey); err != nil {
			return nil, fmt.Errorf("refresh youtube target projection: scan reason: %w", err)
		}
		reason.ObservationKind = contract.ObservationKind(kind)
		reasons = append(reasons, reason)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("refresh youtube target projection: read reasons: %w", err)
	}
	return reasons, nil
}
