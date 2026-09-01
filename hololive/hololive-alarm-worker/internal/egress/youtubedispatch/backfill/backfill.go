package backfill

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
)

const (
	// DefaultBatchSize is the default number of source rows processed per transaction.
	DefaultBatchSize = 500
	maxBatchSize     = 5000
)

// ErrCoverageConfirmationRequired reports inconsistent historical coverage options.
var ErrCoverageConfirmationRequired = errors.New("ledger backfill coverage confirmation is required")

// Options configures a bounded, resumable delivery ledger backfill.
type Options struct {
	BatchSize                 int
	LegacyCoverageStartAt     *time.Time
	HistoricalCoverageChecked bool
}

// Result reports the durable state observed after a run.
type Result struct {
	State     State
	Completed bool
}

// Runner owns the fixed-high-water delivery ledger backfill workflow.
type Runner struct {
	pool    *pgxpool.Pool
	options Options
}

// New validates options and returns a delivery ledger backfill runner.
func New(pool *pgxpool.Pool, options Options) (*Runner, error) {
	if pool == nil {
		return nil, errors.New("new ledger backfill runner: pool is nil")
	}

	if options.BatchSize == 0 {
		options.BatchSize = DefaultBatchSize
	}

	if options.BatchSize < 1 || options.BatchSize > maxBatchSize {
		return nil, fmt.Errorf("new ledger backfill runner: batch size must be between 1 and %d", maxBatchSize)
	}

	if options.LegacyCoverageStartAt != nil {
		normalized := options.LegacyCoverageStartAt.UTC()

		options.LegacyCoverageStartAt = &normalized
	}

	if options.HistoricalCoverageChecked != (options.LegacyCoverageStartAt != nil) {
		return nil, ErrCoverageConfirmationRequired
	}

	return &Runner{pool: pool, options: options}, nil
}

// Run resumes every incomplete pass and records completion only with explicit
// historical coverage evidence.
func (r *Runner) Run(ctx context.Context) (Result, error) {
	state, err := r.Initialize(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("initialize fixed high water: %w", err)
	}

	if state.CompletedAt != nil {
		return Result{State: state, Completed: true}, nil
	}

	if _, passErr := r.runPasses(ctx, state); passErr != nil {
		return Result{}, fmt.Errorf("run ledger backfill passes: %w", passErr)
	}

	state, err = r.completeIfAuthorized(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("complete ledger backfill: %w", err)
	}

	return Result{State: state, Completed: state.CompletedAt != nil}, nil
}

func (r *Runner) runPasses(ctx context.Context, state State) (State, error) {
	state, err := r.runDeliveryPass(ctx, state)
	if err != nil {
		return State{}, fmt.Errorf("run delivery source pass: %w", err)
	}

	state, err = r.runOutboxPass(ctx, state)
	if err != nil {
		return State{}, fmt.Errorf("run outbox source pass: %w", err)
	}

	state, err = r.runVerificationPass(ctx, state)
	if err != nil {
		return State{}, fmt.Errorf("run ledger verification pass: %w", err)
	}

	return state, nil
}

func (r *Runner) runDeliveryPass(ctx context.Context, state State) (State, error) {
	var err error

	for state.DeliveryCursorID < state.DeliveryHighWaterID {
		state, err = r.backfillDeliveryBatch(ctx)
		if err != nil {
			return State{}, fmt.Errorf("backfill delivery batch: %w", err)
		}
	}

	return state, nil
}

func (r *Runner) runOutboxPass(ctx context.Context, state State) (State, error) {
	var err error

	for state.OutboxCursorID < state.OutboxHighWaterID {
		state, err = r.backfillOutboxBatch(ctx)
		if err != nil {
			return State{}, fmt.Errorf("backfill outbox batch: %w", err)
		}
	}

	return state, nil
}

func (r *Runner) runVerificationPass(ctx context.Context, state State) (State, error) {
	var err error

	for state.DeliveryVerifyCursorID < state.DeliveryHighWaterID {
		state, err = r.verifyDeliveryBatch(ctx)
		if err != nil {
			return State{}, fmt.Errorf("verify delivery ledger batch: %w", err)
		}
	}

	return state, nil
}

// CurrentState loads the durable backfill state without changing it.
func (r *Runner) CurrentState(ctx context.Context) (State, error) {
	state, err := loadState(ctx, r.pool)
	if err != nil {
		return State{}, fmt.Errorf("load current ledger backfill state: %w", err)
	}

	return state, nil
}

func validateState(state State) error {
	if state.SchemaVersion != store.LedgerSchemaVersion {
		return fmt.Errorf(
			"unsupported ledger schema version: got %d want %d",
			state.SchemaVersion,
			store.LedgerSchemaVersion,
		)
	}

	if state.DeliveryCursorID > state.DeliveryHighWaterID ||
		state.DeliveryVerifyCursorID > state.DeliveryHighWaterID ||
		state.OutboxCursorID > state.OutboxHighWaterID {
		return errors.New("ledger backfill state cursor exceeds fixed high water")
	}

	return nil
}
