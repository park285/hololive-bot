package backfill

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/dbx"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

func (r *Runner) verifyDeliveryBatch(ctx context.Context) (State, error) {
	var advanced State

	err := deliverysql.InDeliveryTx(ctx, r.pool, func(tx dbx.Querier) error {
		var err error

		advanced, err = r.verifyDeliveryBatchInTx(ctx, tx)
		if err != nil {
			return fmt.Errorf("verify delivery ledger batch transaction: %w", err)
		}

		return nil
	})
	if err != nil {
		return State{}, fmt.Errorf("execute delivery ledger verification transaction: %w", err)
	}

	return advanced, nil
}

func (r *Runner) verifyDeliveryBatchInTx(ctx context.Context, tx dbx.Querier) (State, error) {
	state, err := loadStateForUpdate(ctx, tx)
	if err != nil {
		return State{}, fmt.Errorf("load delivery verification state: %w", err)
	}

	if state.DeliveryCursorID != state.DeliveryHighWaterID ||
		state.OutboxCursorID != state.OutboxHighWaterID {
		return State{}, errors.New("verify delivery ledger: source backfill passes are incomplete")
	}

	if state.DeliveryVerifyCursorID >= state.DeliveryHighWaterID {
		return state, nil
	}

	batch, err := loadDeliverySourceBatch(
		ctx,
		tx,
		state.DeliveryVerifyCursorID,
		state.DeliveryHighWaterID,
		r.options.BatchSize,
	)
	if err != nil {
		return State{}, fmt.Errorf("load delivery verification batch: %w", err)
	}

	expected, err := expectedLedgerStatuses(batch)
	if err != nil {
		return State{}, fmt.Errorf("build expected ledger statuses: %w", err)
	}

	actual, err := loadLedgerStatuses(ctx, tx, expected)
	if err != nil {
		return State{}, fmt.Errorf("load actual ledger statuses: %w", err)
	}

	if verifyErr := verifyLedgerStatuses(expected, actual); verifyErr != nil {
		return State{}, fmt.Errorf("verify delivery ledger statuses: %w", verifyErr)
	}

	advanced, err := advanceCursor(
		ctx,
		tx,
		state,
		deliveryVerifyCursor,
		deliveryBatchCursor(batch, state.DeliveryHighWaterID),
	)
	if err != nil {
		return State{}, fmt.Errorf("advance delivery verification cursor: %w", err)
	}

	return advanced, nil
}

func expectedLedgerStatuses(batch []deliverySourceRow) (map[ytcontentid.LogicalKey]store.LedgerStatus, error) {
	expected := make(map[ytcontentid.LogicalKey]store.LedgerStatus)

	for i := range batch {
		write, status, terminal, err := ledgerWriteForDeliveryRow(batch[i], batch[i].sentAtOrZero())
		if err != nil {
			return nil, fmt.Errorf("resolve delivery verification row %d: %w", batch[i].ID, err)
		}

		if !terminal {
			continue
		}

		if current := expected[write.Key]; current != store.LedgerStatusSent {
			expected[write.Key] = status
		}
	}

	return expected, nil
}

func (row deliverySourceRow) sentAtOrZero() time.Time {
	if row.SentAt == nil {
		return time.Time{}
	}

	return *row.SentAt
}

func verifyLedgerStatuses(
	expected map[ytcontentid.LogicalKey]store.LedgerStatus,
	actual map[ytcontentid.LogicalKey]store.LedgerStatus,
) error {
	for _, key := range sortedLogicalKeys(expected) {
		actualStatus, ok := actual[key]
		if !ok {
			return fmt.Errorf("delivery ledger anti-join mismatch for key %s: row is missing", key.Hash())
		}

		if !ledgerStatusSatisfies(expected[key], actualStatus) {
			return fmt.Errorf(
				"delivery ledger status mismatch for key %s: got %s want %s",
				key.Hash(),
				actualStatus,
				expected[key],
			)
		}
	}

	return nil
}

func ledgerStatusSatisfies(expected, actual store.LedgerStatus) bool {
	return actual == expected ||
		(expected == store.LedgerStatusQuarantined && actual == store.LedgerStatusSent)
}

func loadLedgerStatuses(
	ctx context.Context,
	tx dbx.Querier,
	expected map[ytcontentid.LogicalKey]store.LedgerStatus,
) (map[ytcontentid.LogicalKey]store.LedgerStatus, error) {
	actual := make(map[ytcontentid.LogicalKey]store.LedgerStatus, len(expected))
	if len(expected) == 0 {
		return actual, nil
	}

	kinds, logicalIDs, roomIDs := ledgerKeyArrays(sortedLogicalKeys(expected))

	rows, err := tx.Query(ctx, mustSQL("ledger_statuses.sql"), kinds, logicalIDs, roomIDs)
	if err != nil {
		return nil, fmt.Errorf("select delivery ledger verification rows: %w", err)
	}
	defer rows.Close()

	if err := scanLedgerStatuses(rows, actual); err != nil {
		return nil, fmt.Errorf("scan delivery ledger statuses: %w", err)
	}

	return actual, nil
}

func sortedLogicalKeys(expected map[ytcontentid.LogicalKey]store.LedgerStatus) []ytcontentid.LogicalKey {
	keys := make([]ytcontentid.LogicalKey, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}

	slices.SortFunc(keys, compareLogicalKeys)

	return keys
}

func ledgerKeyArrays(keys []ytcontentid.LogicalKey) ([]string, []string, []string) {
	kinds := make([]string, 0, len(keys))
	logicalIDs := make([]string, 0, len(keys))
	roomIDs := make([]string, 0, len(keys))

	for _, key := range keys {
		kinds = append(kinds, string(key.Kind))
		logicalIDs = append(logicalIDs, key.LogicalID)
		roomIDs = append(roomIDs, key.RoomID)
	}

	return kinds, logicalIDs, roomIDs
}

type ledgerRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanLedgerStatuses(
	rows ledgerRows,
	actual map[ytcontentid.LogicalKey]store.LedgerStatus,
) error {
	for rows.Next() {
		var (
			key    ytcontentid.LogicalKey
			status store.LedgerStatus
		)

		if err := rows.Scan(&key.Kind, &key.LogicalID, &key.RoomID, &status); err != nil {
			return fmt.Errorf("scan delivery ledger verification row: %w", err)
		}

		if status != store.LedgerStatusSent && status != store.LedgerStatusQuarantined {
			return fmt.Errorf("delivery ledger key %s has unsupported status %q", key.Hash(), status)
		}

		actual[key] = status
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate delivery ledger verification rows: %w", err)
	}

	return nil
}
