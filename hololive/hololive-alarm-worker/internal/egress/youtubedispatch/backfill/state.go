package backfill

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

// State is the durable fixed-high-water backfill progress record.
type State struct {
	SchemaVersion          int
	DeliveryHighWaterID    int64
	OutboxHighWaterID      int64
	DeliveryCursorID       int64
	DeliveryVerifyCursorID int64
	OutboxCursorID         int64
	LegacyCoverageStartAt  *time.Time
	CoverageVerifiedAt     *time.Time
	StartedAt              time.Time
	CompletedAt            *time.Time
	UpdatedAt              time.Time
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanState(row rowScanner) (State, error) {
	var state State

	err := row.Scan(
		&state.SchemaVersion,
		&state.DeliveryHighWaterID,
		&state.OutboxHighWaterID,
		&state.DeliveryCursorID,
		&state.DeliveryVerifyCursorID,
		&state.OutboxCursorID,
		&state.LegacyCoverageStartAt,
		&state.CoverageVerifiedAt,
		&state.StartedAt,
		&state.CompletedAt,
		&state.UpdatedAt,
	)
	if err != nil {
		return State{}, fmt.Errorf("scan ledger backfill state: %w", err)
	}

	return state, nil
}

func loadState(ctx context.Context, db dbx.Querier) (State, error) {
	state, err := scanState(db.QueryRow(ctx, mustSQL("state_load.sql")))
	if err != nil {
		return State{}, fmt.Errorf("load ledger backfill state: %w", err)
	}

	if err := validateState(state); err != nil {
		return State{}, fmt.Errorf("validate loaded ledger backfill state: %w", err)
	}

	return state, nil
}

func loadStateForUpdate(ctx context.Context, tx dbx.Querier) (State, error) {
	state, err := scanState(tx.QueryRow(ctx, mustSQL("state_load_for_update.sql")))
	if err != nil {
		return State{}, fmt.Errorf("load ledger backfill state for update: %w", err)
	}

	if err := validateState(state); err != nil {
		return State{}, fmt.Errorf("validate locked ledger backfill state: %w", err)
	}

	return state, nil
}

// Initialize captures immutable source high-water marks exactly once.
func (r *Runner) Initialize(ctx context.Context) (State, error) {
	var initialized State

	err := deliverysql.InDeliveryTx(ctx, r.pool, func(tx dbx.Querier) error {
		var err error

		initialized, err = initializeInTx(ctx, tx)
		if err != nil {
			return fmt.Errorf("initialize ledger backfill transaction: %w", err)
		}

		return nil
	})
	if err != nil {
		return State{}, fmt.Errorf("execute ledger backfill initialization: %w", err)
	}

	return initialized, nil
}

func initializeInTx(ctx context.Context, tx dbx.Querier) (State, error) {
	if _, err := tx.Exec(ctx, mustSQL("state_initialize_lock.sql")); err != nil {
		return State{}, fmt.Errorf("lock ledger backfill initialization: %w", err)
	}

	state, err := loadStateForUpdate(ctx, tx)
	if err == nil {
		return state, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return State{}, fmt.Errorf("load existing state: %w", err)
	}

	state, err = captureInitialState(ctx, tx)
	if err != nil {
		return State{}, fmt.Errorf("capture initial ledger backfill state: %w", err)
	}

	return state, nil
}

func captureInitialState(ctx context.Context, tx dbx.Querier) (State, error) {
	var (
		deliveryHighWaterID, outboxHighWaterID int64
		startedAt                              time.Time
	)

	if err := tx.QueryRow(ctx, mustSQL("state_capture_high_water.sql")).Scan(
		&deliveryHighWaterID,
		&outboxHighWaterID,
		&startedAt,
	); err != nil {
		return State{}, fmt.Errorf("capture fixed high water: %w", err)
	}

	state, err := scanState(tx.QueryRow(ctx, mustSQL("state_insert.sql"),
		store.LedgerSchemaVersion,
		deliveryHighWaterID,
		outboxHighWaterID,
		startedAt,
	))
	if err != nil {
		return State{}, fmt.Errorf("insert fixed high water state: %w", err)
	}

	return state, nil
}

type cursorKind string

const (
	deliveryCursor       cursorKind = "delivery_cursor_id"
	deliveryVerifyCursor cursorKind = "delivery_verify_cursor_id"
	outboxCursor         cursorKind = "outbox_cursor_id"
)

func advanceCursor(ctx context.Context, tx dbx.Querier, state State, cursor cursorKind, next int64) (State, error) {
	spec, err := cursorSpecFor(state, cursor)
	if err != nil {
		return State{}, fmt.Errorf("resolve ledger backfill cursor: %w", err)
	}

	if next < spec.current || next > spec.highWater {
		return State{}, fmt.Errorf(
			"advance ledger backfill %s: next %d outside [%d,%d]",
			cursor,
			next,
			spec.current,
			spec.highWater,
		)
	}

	updated, err := scanState(tx.QueryRow(
		ctx,
		mustSQL(spec.queryName),
		next,
		store.LedgerSchemaVersion,
		spec.current,
	))
	if err != nil {
		return State{}, fmt.Errorf("advance ledger backfill %s: %w", cursor, err)
	}

	return updated, nil
}

type cursorSpec struct {
	current   int64
	highWater int64
	queryName string
}

func cursorSpecFor(state State, cursor cursorKind) (cursorSpec, error) {
	switch cursor {
	case deliveryCursor:
		return cursorSpec{state.DeliveryCursorID, state.DeliveryHighWaterID, "state_advance_delivery.sql"}, nil
	case deliveryVerifyCursor:
		return cursorSpec{
			state.DeliveryVerifyCursorID,
			state.DeliveryHighWaterID,
			"state_advance_delivery_verify.sql",
		}, nil
	case outboxCursor:
		return cursorSpec{state.OutboxCursorID, state.OutboxHighWaterID, "state_advance_outbox.sql"}, nil
	default:
		return cursorSpec{}, fmt.Errorf("advance ledger backfill cursor: unsupported cursor %q", cursor)
	}
}

func (r *Runner) completeIfAuthorized(ctx context.Context) (State, error) {
	if r.options.LegacyCoverageStartAt == nil {
		state, err := r.CurrentState(ctx)
		if err != nil {
			return State{}, fmt.Errorf("load incomplete ledger backfill state: %w", err)
		}

		return state, nil
	}

	var completed State

	err := deliverysql.InDeliveryTx(ctx, r.pool, func(tx dbx.Querier) error {
		var err error

		completed, err = completeInTx(ctx, tx, *r.options.LegacyCoverageStartAt)
		if err != nil {
			return fmt.Errorf("complete ledger backfill transaction: %w", err)
		}

		return nil
	})
	if err != nil {
		return State{}, fmt.Errorf("execute ledger backfill completion: %w", err)
	}

	return completed, nil
}

func completeInTx(ctx context.Context, tx dbx.Querier, coverageStart time.Time) (State, error) {
	state, err := loadStateForUpdate(ctx, tx)
	if err != nil {
		return State{}, fmt.Errorf("load completion state: %w", err)
	}

	if state.CompletedAt != nil {
		return state, nil
	}

	if validationErr := validateCompletionState(state, coverageStart); validationErr != nil {
		return State{}, fmt.Errorf("validate ledger backfill completion: %w", validationErr)
	}

	completed, err := scanState(tx.QueryRow(
		ctx,
		mustSQL("state_complete.sql"),
		coverageStart,
		store.LedgerSchemaVersion,
	))
	if err != nil {
		return State{}, fmt.Errorf("write ledger backfill completion: %w", err)
	}

	return completed, nil
}

func validateCompletionState(state State, coverageStart time.Time) error {
	if state.DeliveryCursorID != state.DeliveryHighWaterID ||
		state.DeliveryVerifyCursorID != state.DeliveryHighWaterID ||
		state.OutboxCursorID != state.OutboxHighWaterID {
		return errors.New("ledger backfill cursors have not reached fixed high water")
	}

	if state.LegacyCoverageStartAt != nil && !state.LegacyCoverageStartAt.Equal(coverageStart) {
		return errors.New("ledger backfill coverage start is immutable")
	}

	return nil
}
