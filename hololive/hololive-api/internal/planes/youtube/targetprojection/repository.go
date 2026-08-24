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
		return nil, errors.New("create youtube target projection refresher: pool is nil")
	}

	if validity < MinValidity || validity > MaxValidity {
		return nil, fmt.Errorf("create youtube target projection refresher: validity must be between %s and %s", MinValidity, MaxValidity)
	}

	return &Refresher{pool: pool, validity: validity}, nil
}

func (r *Refresher) Refresh(ctx context.Context, builder Builder, now time.Time) (Result, error) {
	if r == nil || r.pool == nil || builder == nil || now.IsZero() {
		return Result{}, errors.New("refresh youtube target projection: invalid refresher, builder, or time")
	}

	now = now.UTC()

	validUntil := now.Add(r.validity)

	out, err := dbx.InPgxTxWithResult(ctx, r.pool, func(tx dbx.Tx) (Result, error) {
		return buildAndRefreshProjection(ctx, tx, builder, now, validUntil)
	})
	if err != nil {
		return out, fmt.Errorf("in pgx tx with result: %w", err)
	}

	return out, nil
}

func buildAndRefreshProjection(ctx context.Context, tx dbx.Tx, builder Builder, now, validUntil time.Time) (Result, error) {
	targets, reasons, err := builder.Build(ctx, tx, now)
	if err != nil {
		return Result{}, fmt.Errorf("refresh youtube target projection: build: %w", err)
	}

	targets, reasons, hash, err := normalize(targets, reasons)
	if err != nil {
		return Result{}, fmt.Errorf("refresh youtube target projection: normalize: %w", err)
	}

	out, err := refreshTx(ctx, tx, targets, reasons, hash, now, validUntil)
	if err != nil {
		return out, fmt.Errorf("%w", err)
	}

	return out, nil
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
		return Result{}, fmt.Errorf("lock current: %w", err)
	}

	if sameProjection(current, targets, hash) {
		out, extendErr := extendUnchangedGeneration(ctx, tx, current, targets, reasons, hash, validUntil)
		if extendErr != nil {
			return out, fmt.Errorf("extend unchanged generation: %w", extendErr)
		}

		return out, nil
	}

	out, err := activateStagingGeneration(ctx, tx, current, targets, reasons, hash, now, validUntil)
	if err != nil {
		return out, fmt.Errorf("activate staging generation: %w", err)
	}

	return out, nil
}

func sameProjection(current currentProjection, targets []TargetSpec, hash string) bool {
	return current.found && current.rowCount == len(targets) && current.hash == hash
}

func extendUnchangedGeneration(
	ctx context.Context,
	tx dbx.Tx,
	current currentProjection,
	targets []TargetSpec,
	reasons []TargetReason,
	hash string,
	validUntil time.Time,
) (Result, error) {
	if _, err := tx.Exec(ctx, mustSQL("extend_generation_validity.sql"), current.generation, validUntil); err != nil {
		return Result{}, fmt.Errorf("refresh youtube target projection: extend generation validity: %w", err)
	}

	if _, err := tx.Exec(ctx, mustSQL("extend_target_validity.sql"), current.generation, validUntil); err != nil {
		return Result{}, fmt.Errorf("refresh youtube target projection: extend target validity: %w", err)
	}

	if err := replaceReasonsIfChanged(ctx, tx, current.generation, reasons); err != nil {
		return Result{}, fmt.Errorf("replace reasons if changed: %w", err)
	}

	return Result{Generation: current.generation, RowCount: len(targets), SHA256: hash}, nil
}

func replaceReasonsIfChanged(ctx context.Context, tx dbx.Tx, generation int64, reasons []TargetReason) error {
	storedReasons, err := loadReasons(ctx, tx, generation)
	if err != nil {
		return fmt.Errorf("load reasons: %w", err)
	}

	if sameReasons(storedReasons, reasons) {
		return nil
	}

	if _, err := tx.Exec(ctx, mustSQL("delete_reasons.sql"), generation); err != nil {
		return fmt.Errorf("refresh youtube target projection: replace reasons: %w", err)
	}

	if err := insertReasons(ctx, tx, generation, reasons); err != nil {
		return fmt.Errorf("insert reasons: %w", err)
	}

	return nil
}

func activateStagingGeneration(
	ctx context.Context,
	tx dbx.Tx,
	current currentProjection,
	targets []TargetSpec,
	reasons []TargetReason,
	hash string,
	now, validUntil time.Time,
) (Result, error) {
	generation, err := insertStagingProjection(ctx, tx, targets, reasons, hash, validUntil)
	if err != nil {
		return Result{}, fmt.Errorf("insert staging projection: %w", err)
	}

	if err := verifyStagingProjection(ctx, tx, generation, targets, reasons, hash); err != nil {
		return Result{}, fmt.Errorf("verify staging projection: %w", err)
	}

	if err := promoteStagingGeneration(ctx, tx, current, generation, now); err != nil {
		return Result{}, fmt.Errorf("promote staging generation: %w", err)
	}

	return Result{Generation: generation, RowCount: len(targets), SHA256: hash, Changed: true}, nil
}

func insertStagingProjection(
	ctx context.Context,
	tx dbx.Tx,
	targets []TargetSpec,
	reasons []TargetReason,
	hash string,
	validUntil time.Time,
) (int64, error) {
	var generation int64

	err := tx.QueryRow(ctx, mustSQL("insert_staging_generation.sql"), len(targets), hash, validUntil).Scan(&generation)
	if err != nil {
		return 0, fmt.Errorf("refresh youtube target projection: insert staging generation: %w", err)
	}

	if err := insertTargets(ctx, tx, generation, validUntil, targets); err != nil {
		return 0, fmt.Errorf("insert targets: %w", err)
	}

	if err := insertReasons(ctx, tx, generation, reasons); err != nil {
		return 0, fmt.Errorf("insert reasons: %w", err)
	}

	return generation, nil
}

func verifyStagingProjection(
	ctx context.Context,
	tx dbx.Tx,
	generation int64,
	targets []TargetSpec,
	reasons []TargetReason,
	hash string,
) error {
	loadedTargets, err := loadTargets(ctx, tx, generation)
	if err != nil {
		return fmt.Errorf("load targets: %w", err)
	}

	loadedReasons, err := loadReasons(ctx, tx, generation)
	if err != nil {
		return fmt.Errorf("load reasons: %w", err)
	}

	_, _, storedHash, err := normalize(loadedTargets, reasons)
	if err != nil || len(loadedTargets) != len(targets) || storedHash != hash || !sameReasons(loadedReasons, reasons) {
		return fmt.Errorf("refresh youtube target projection: staging verification failed: %w", ErrInvalidProjection)
	}

	return nil
}

func promoteStagingGeneration(ctx context.Context, tx dbx.Tx, current currentProjection, generation int64, now time.Time) error {
	if current.found {
		if _, err := tx.Exec(ctx, mustSQL("retire_current_generation.sql"), current.generation); err != nil {
			return fmt.Errorf("refresh youtube target projection: retire current generation: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, mustSQL("activate_staging_generation.sql"), generation, now); err != nil {
		return fmt.Errorf("refresh youtube target projection: activate staging generation: %w", err)
	}

	return nil
}

func lockCurrent(ctx context.Context, tx dbx.Tx) (currentProjection, error) {
	var current currentProjection

	err := tx.QueryRow(ctx, mustSQL("lock_current_generation.sql")).Scan(&current.generation, &current.rowCount, &current.hash)

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

	if _, err := tx.Exec(ctx, mustSQL("insert_targets.sql"), generation, subjects, kinds, priorities, intervals, enabled, validUntil); err != nil {
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

	if _, err := tx.Exec(ctx, mustSQL("insert_reasons.sql"), generation, subjects, kinds, reasonKinds, reasonKeys); err != nil {
		return fmt.Errorf("refresh youtube target projection: insert reasons: %w", err)
	}

	return nil
}

func loadTargets(ctx context.Context, tx dbx.Tx, generation int64) ([]TargetSpec, error) {
	rows, err := tx.Query(ctx, mustSQL("load_targets.sql"), generation)
	if err != nil {
		return nil, fmt.Errorf("refresh youtube target projection: verify target rows: %w", err)
	}
	defer rows.Close()

	targets := make([]TargetSpec, 0)

	for rows.Next() {
		var (
			target     TargetSpec
			kind       string
			intervalMS int64
		)

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
	rows, err := tx.Query(ctx, mustSQL("load_reasons.sql"), generation)
	if err != nil {
		return nil, fmt.Errorf("refresh youtube target projection: load reasons: %w", err)
	}
	defer rows.Close()

	reasons := make([]TargetReason, 0)

	for rows.Next() {
		var (
			reason TargetReason
			kind   string
		)

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
