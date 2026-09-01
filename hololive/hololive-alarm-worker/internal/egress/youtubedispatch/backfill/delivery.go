package backfill

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

type deliverySourceRow struct {
	ID        int64
	Kind      domain.OutboxKind
	ContentID string
	Payload   string
	RoomID    string
	Status    domain.OutboxStatus
	SentAt    *time.Time
}

func (r *Runner) backfillDeliveryBatch(ctx context.Context) (State, error) {
	var advanced State

	err := deliverysql.InDeliveryTx(ctx, r.pool, func(tx dbx.Querier) error {
		var err error

		advanced, err = r.backfillDeliveryBatchInTx(ctx, tx)
		if err != nil {
			return fmt.Errorf("backfill delivery batch transaction: %w", err)
		}

		return nil
	})
	if err != nil {
		return State{}, fmt.Errorf("execute delivery backfill transaction: %w", err)
	}

	return advanced, nil
}

func (r *Runner) backfillDeliveryBatchInTx(ctx context.Context, tx dbx.Querier) (State, error) {
	state, err := loadStateForUpdate(ctx, tx)
	if err != nil {
		return State{}, fmt.Errorf("load delivery backfill state: %w", err)
	}

	if state.DeliveryCursorID >= state.DeliveryHighWaterID {
		return state, nil
	}

	batch, err := loadDeliverySourceBatch(
		ctx,
		tx,
		state.DeliveryCursorID,
		state.DeliveryHighWaterID,
		r.options.BatchSize,
	)
	if err != nil {
		return State{}, fmt.Errorf("load delivery source batch: %w", err)
	}

	quarantined, sent, err := ledgerWritesForDeliveryBatch(batch, state.StartedAt)
	if err != nil {
		return State{}, fmt.Errorf("build delivery ledger writes: %w", err)
	}

	if recordErr := recordDeliverySourceRows(ctx, tx, quarantined, sent); recordErr != nil {
		return State{}, fmt.Errorf("record delivery source rows: %w", recordErr)
	}

	advanced, err := advanceCursor(
		ctx,
		tx,
		state,
		deliveryCursor,
		deliveryBatchCursor(batch, state.DeliveryHighWaterID),
	)
	if err != nil {
		return State{}, fmt.Errorf("advance delivery backfill cursor: %w", err)
	}

	return advanced, nil
}

func loadDeliverySourceBatch(
	ctx context.Context,
	tx dbx.Querier,
	cursor int64,
	highWater int64,
	batchSize int,
) ([]deliverySourceRow, error) {
	rows, err := tx.Query(
		ctx,
		mustSQL("delivery_batch.sql"),
		cursor,
		highWater,
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("select delivery source batch: %w", err)
	}
	defer rows.Close()

	batch := make([]deliverySourceRow, 0, batchSize)

	for rows.Next() {
		var row deliverySourceRow

		if err := rows.Scan(
			&row.ID,
			&row.Kind,
			&row.ContentID,
			&row.Payload,
			&row.RoomID,
			&row.Status,
			&row.SentAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery source row: %w", err)
		}

		batch = append(batch, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery source batch: %w", err)
	}

	return batch, nil
}

func ledgerWritesForDeliveryBatch(
	batch []deliverySourceRow,
	startedAt time.Time,
) (map[ytcontentid.LogicalKey]store.LedgerWrite, map[ytcontentid.LogicalKey]store.LedgerWrite, error) {
	quarantined := make(map[ytcontentid.LogicalKey]store.LedgerWrite)
	sent := make(map[ytcontentid.LogicalKey]store.LedgerWrite)

	for i := range batch {
		write, status, terminal, err := ledgerWriteForDeliveryRow(batch[i], startedAt)
		if err != nil {
			return nil, nil, fmt.Errorf("build ledger write for delivery row %d: %w", batch[i].ID, err)
		}

		if !terminal {
			continue
		}

		if status == store.LedgerStatusSent {
			retainEarliestWrite(sent, write)
		} else {
			retainEarliestWrite(quarantined, write)
		}
	}

	return quarantined, sent, nil
}

func ledgerWriteForDeliveryRow(
	row deliverySourceRow,
	startedAt time.Time,
) (store.LedgerWrite, store.LedgerStatus, bool, error) {
	var status store.LedgerStatus

	switch row.Status {
	case domain.OutboxStatusPending, store.DeliveryStatusSending, domain.OutboxStatusFailed:
		return store.LedgerWrite{}, "", false, nil
	case domain.OutboxStatusSent:
		status = store.LedgerStatusSent
	case store.DeliveryStatusQuarantined:
		status = store.LedgerStatusQuarantined
	default:
		return store.LedgerWrite{}, "", false, fmt.Errorf(
			"delivery source row %d has unsupported status %q",
			row.ID,
			row.Status,
		)
	}

	key, err := ytcontentid.ResolveDeliveryKey(row.Kind, row.ContentID, row.Payload, row.RoomID)
	if err != nil {
		return store.LedgerWrite{}, "", false, fmt.Errorf("resolve delivery source row %d: %w", row.ID, err)
	}

	return store.LedgerWrite{
		Key:              key,
		ObservedAt:       deliveryObservedAt(row, startedAt),
		SourceDeliveryID: row.ID,
	}, status, true, nil
}

func deliveryObservedAt(row deliverySourceRow, fallback time.Time) time.Time {
	if row.Status == domain.OutboxStatusSent && row.SentAt != nil {
		return row.SentAt.UTC()
	}

	return fallback.UTC()
}

func recordDeliverySourceRows(
	ctx context.Context,
	tx dbx.Querier,
	quarantined map[ytcontentid.LogicalKey]store.LedgerWrite,
	sent map[ytcontentid.LogicalKey]store.LedgerWrite,
) error {
	if err := store.RecordDeliveryLedgerWrites(
		ctx,
		tx,
		store.LedgerStatusQuarantined,
		sortedLedgerWrites(quarantined),
	); err != nil {
		return fmt.Errorf("record quarantined delivery source rows: %w", err)
	}

	if err := store.RecordDeliveryLedgerWrites(
		ctx,
		tx,
		store.LedgerStatusSent,
		sortedLedgerWrites(sent),
	); err != nil {
		return fmt.Errorf("record sent delivery source rows: %w", err)
	}

	return nil
}

func deliveryBatchCursor(batch []deliverySourceRow, highWater int64) int64 {
	if len(batch) == 0 {
		return highWater
	}

	return batch[len(batch)-1].ID
}

func retainEarliestWrite(byKey map[ytcontentid.LogicalKey]store.LedgerWrite, candidate store.LedgerWrite) {
	current, ok := byKey[candidate.Key]
	if !ok || candidate.ObservedAt.Before(current.ObservedAt) ||
		(candidate.ObservedAt.Equal(current.ObservedAt) && candidate.SourceDeliveryID < current.SourceDeliveryID) {
		byKey[candidate.Key] = candidate
	}
}

func sortedLedgerWrites(byKey map[ytcontentid.LogicalKey]store.LedgerWrite) []store.LedgerWrite {
	writes := make([]store.LedgerWrite, 0, len(byKey))
	for _, write := range byKey {
		writes = append(writes, write)
	}

	slices.SortFunc(writes, func(a, b store.LedgerWrite) int {
		return compareLogicalKeys(a.Key, b.Key)
	})

	return writes
}

func compareLogicalKeys(a, b ytcontentid.LogicalKey) int {
	if byKind := cmp.Compare(a.Kind, b.Kind); byKind != 0 {
		return byKind
	}

	if byLogicalID := cmp.Compare(a.LogicalID, b.LogicalID); byLogicalID != 0 {
		return byLogicalID
	}

	return cmp.Compare(a.RoomID, b.RoomID)
}
