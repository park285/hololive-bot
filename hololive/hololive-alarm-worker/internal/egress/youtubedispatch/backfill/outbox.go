package backfill

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

type outboxSourceRow struct {
	ID                   int64
	Status               domain.OutboxStatus
	SentAt               *time.Time
	LatestDeliverySentAt *time.Time
}

func (r *Runner) backfillOutboxBatch(ctx context.Context) (State, error) {
	var advanced State

	err := deliverysql.InDeliveryTx(ctx, r.pool, func(tx dbx.Querier) error {
		var err error

		advanced, err = r.backfillOutboxBatchInTx(ctx, tx)
		if err != nil {
			return fmt.Errorf("backfill outbox batch transaction: %w", err)
		}

		return nil
	})
	if err != nil {
		return State{}, fmt.Errorf("execute outbox backfill transaction: %w", err)
	}

	return advanced, nil
}

func (r *Runner) backfillOutboxBatchInTx(ctx context.Context, tx dbx.Querier) (State, error) {
	state, err := loadStateForUpdate(ctx, tx)
	if err != nil {
		return State{}, fmt.Errorf("load outbox backfill state: %w", err)
	}

	if state.OutboxCursorID >= state.OutboxHighWaterID {
		return state, nil
	}

	batch, err := loadOutboxSourceBatch(ctx, tx, state, r.options.BatchSize)
	if err != nil {
		return State{}, fmt.Errorf("load outbox source batch: %w", err)
	}

	pendingIDs, terminalIDs, terminalAts, err := classifyOutboxBatch(batch, state.StartedAt)
	if err != nil {
		return State{}, fmt.Errorf("classify outbox source batch: %w", err)
	}

	if updateErr := applyOutboxTerminalUpdates(ctx, tx, pendingIDs, terminalIDs, terminalAts); updateErr != nil {
		return State{}, fmt.Errorf("apply outbox terminal timestamps: %w", updateErr)
	}

	advanced, err := advanceCursor(ctx, tx, state, outboxCursor, outboxBatchCursor(batch, state.OutboxHighWaterID))
	if err != nil {
		return State{}, fmt.Errorf("advance outbox backfill cursor: %w", err)
	}

	return advanced, nil
}

func loadOutboxSourceBatch(
	ctx context.Context,
	tx dbx.Querier,
	state State,
	batchSize int,
) ([]outboxSourceRow, error) {
	rows, err := tx.Query(
		ctx,
		mustSQL("outbox_batch.sql"),
		state.OutboxCursorID,
		state.OutboxHighWaterID,
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("select outbox source batch: %w", err)
	}
	defer rows.Close()

	batch := make([]outboxSourceRow, 0, batchSize)

	for rows.Next() {
		var row outboxSourceRow

		if err := rows.Scan(&row.ID, &row.Status, &row.SentAt, &row.LatestDeliverySentAt); err != nil {
			return nil, fmt.Errorf("scan outbox source row: %w", err)
		}

		batch = append(batch, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox source batch: %w", err)
	}

	return batch, nil
}

func classifyOutboxBatch(
	batch []outboxSourceRow,
	startedAt time.Time,
) ([]int64, []int64, []time.Time, error) {
	pendingIDs := make([]int64, 0, len(batch))
	terminalIDs := make([]int64, 0, len(batch))
	terminalAts := make([]time.Time, 0, len(batch))

	for i := range batch {
		row := batch[i]

		terminalAt, terminal, err := terminalAtForOutboxRow(row, startedAt)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("classify outbox source row %d: %w", row.ID, err)
		}

		if !terminal {
			pendingIDs = append(pendingIDs, row.ID)

			continue
		}

		terminalIDs = append(terminalIDs, row.ID)
		terminalAts = append(terminalAts, terminalAt)
	}

	return pendingIDs, terminalIDs, terminalAts, nil
}

func terminalAtForOutboxRow(row outboxSourceRow, startedAt time.Time) (time.Time, bool, error) {
	var terminalAt time.Time

	switch row.Status {
	case domain.OutboxStatusPending:
		return time.Time{}, false, nil
	case domain.OutboxStatusSent:
		terminalAt = sentTerminalAt(row, startedAt)
	case domain.OutboxStatusFailed:
		terminalAt = startedAt.UTC()
	default:
		return time.Time{}, false, fmt.Errorf("outbox source row %d has unsupported status %q", row.ID, row.Status)
	}

	return terminalAt, true, nil
}

func applyOutboxTerminalUpdates(
	ctx context.Context,
	tx dbx.Querier,
	pendingIDs []int64,
	terminalIDs []int64,
	terminalAts []time.Time,
) error {
	if len(pendingIDs) > 0 {
		if _, err := tx.Exec(ctx, mustSQL("outbox_clear_pending_terminal.sql"), pendingIDs); err != nil {
			return fmt.Errorf("clear pending outbox terminal timestamps: %w", err)
		}
	}

	if len(terminalIDs) > 0 {
		if _, err := tx.Exec(ctx, mustSQL("outbox_set_terminal.sql"), terminalIDs, terminalAts); err != nil {
			return fmt.Errorf("backfill terminal outbox timestamps: %w", err)
		}
	}

	return nil
}

func outboxBatchCursor(batch []outboxSourceRow, highWater int64) int64 {
	if len(batch) == 0 {
		return highWater
	}

	return batch[len(batch)-1].ID
}

func sentTerminalAt(row outboxSourceRow, fallback time.Time) time.Time {
	terminalAt := fallback.UTC()

	if row.SentAt != nil {
		terminalAt = row.SentAt.UTC()
	}

	if row.LatestDeliverySentAt != nil && row.LatestDeliverySentAt.After(terminalAt) {
		terminalAt = row.LatestDeliverySentAt.UTC()
	}

	return terminalAt
}
