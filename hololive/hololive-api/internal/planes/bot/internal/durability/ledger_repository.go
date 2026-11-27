package durability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type DurableLedgerMaintenance struct {
	DeletedInbox   int64
	DeletedCommand int64
	DeletedOutbox  int64
}

type DurableLedgerRepository struct{ store *Store }

func NewDurableLedgerRepository(store *Store) *DurableLedgerRepository {
	return &DurableLedgerRepository{store: store}
}

func (r *DurableLedgerRepository) Maintain(
	ctx context.Context,
	terminalRetention time.Duration,
	manualReviewRetention time.Duration,
	batchSize int32,
) (DurableLedgerMaintenance, error) {
	if err := ensureStore(r.store); err != nil {
		return DurableLedgerMaintenance{}, err
	}

	terminalMS, err := leaseMilliseconds(terminalRetention)
	if err != nil {
		return DurableLedgerMaintenance{}, err
	}

	manualReviewMS, err := leaseMilliseconds(manualReviewRetention)
	if err != nil {
		return DurableLedgerMaintenance{}, err
	}

	if manualReviewRetention < terminalRetention {
		return DurableLedgerMaintenance{}, errors.Join(ErrInvalidArgument,
			errors.New("manual review retention must not be shorter than terminal retention"))
	}

	if batchSize <= 0 {
		return DurableLedgerMaintenance{}, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	var result DurableLedgerMaintenance

	err = r.store.write(ctx, func(tx *sql.Tx, now int64) error {
		terminalCutoff, manualReviewCutoff := now-terminalMS, now-manualReviewMS

		var pruneErr error

		if result.DeletedInbox, pruneErr = deleteCount(ctx, tx, durablePruneInboxSQL, terminalCutoff, batchSize); pruneErr != nil {
			return pruneErr
		}

		if result.DeletedCommand, pruneErr = deleteCount(ctx, tx, durablePruneCommandSQL, terminalCutoff, batchSize); pruneErr != nil {
			return pruneErr
		}

		result.DeletedOutbox, pruneErr = deleteCount(ctx, tx, durablePruneOutboxSQL, terminalCutoff, manualReviewCutoff, batchSize)

		return pruneErr
	})
	if err != nil {
		return DurableLedgerMaintenance{}, fmt.Errorf("prune durable terminal ledgers: %w", err)
	}

	return result, nil
}

func deleteCount(ctx context.Context, tx *sql.Tx, query string, args ...any) (int64, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete terminal ledger rows: %w", err)
	}

	return rowsAffected(result)
}
