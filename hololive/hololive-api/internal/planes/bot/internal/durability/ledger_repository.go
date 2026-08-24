package durability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var durablePruneTerminalSQL = mustSQL("durable_prune_terminal.sql")

type DurableLedgerMaintenance struct {
	DeletedInbox   int64
	DeletedCommand int64
	DeletedOutbox  int64
}

type DurableLedgerRepository struct{ pool *pgxpool.Pool }

func NewDurableLedgerRepository(pool *pgxpool.Pool) *DurableLedgerRepository {
	return &DurableLedgerRepository{pool: pool}
}

func (r *DurableLedgerRepository) Maintain(
	ctx context.Context,
	terminalRetention time.Duration,
	manualReviewRetention time.Duration,
	batchSize int32,
) (DurableLedgerMaintenance, error) {
	if err := ensurePool(r.pool); err != nil {
		return DurableLedgerMaintenance{}, fmt.Errorf("ensure pool: %w", err)
	}

	terminalMS, err := leaseMilliseconds(terminalRetention)
	if err != nil {
		return DurableLedgerMaintenance{}, fmt.Errorf("lease milliseconds: %w", err)
	}

	manualReviewMS, err := leaseMilliseconds(manualReviewRetention)
	if err != nil {
		return DurableLedgerMaintenance{}, fmt.Errorf("lease milliseconds: %w", err)
	}

	if manualReviewRetention < terminalRetention {
		return DurableLedgerMaintenance{}, errors.Join(ErrInvalidArgument,
			errors.New("manual review retention must not be shorter than terminal retention"))
	}

	if batchSize <= 0 {
		return DurableLedgerMaintenance{}, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	var result DurableLedgerMaintenance

	err = r.pool.QueryRow(ctx, durablePruneTerminalSQL, terminalMS, manualReviewMS, batchSize).
		Scan(&result.DeletedInbox, &result.DeletedCommand, &result.DeletedOutbox)
	if err != nil {
		return DurableLedgerMaintenance{}, fmt.Errorf("prune durable terminal ledgers: %w", err)
	}

	return result, nil
}
