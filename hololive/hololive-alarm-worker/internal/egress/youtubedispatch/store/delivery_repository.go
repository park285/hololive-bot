package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type DeliveryRepository struct {
	db     deliverysql.DeliveryDB
	logger *slog.Logger
}

func NewDeliveryRepository(db deliverysql.DeliveryDB, logger *slog.Logger) *DeliveryRepository {
	return &DeliveryRepository{
		db:     db,
		logger: logger,
	}
}

const outboxAggregateFailedErrorText = "per-room delivery failed"

func (r *DeliveryRepository) UpdateOutboxAggregateStatuses(ctx context.Context, outboxIDs []int64) error {
	uniqueIDs := deliverysql.UniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	if err := pgx.BeginTxFunc(ctx, r.db, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(tx pgx.Tx) error {
		if err := lockOutboxAggregateParents(ctx, tx, uniqueIDs); err != nil {
			return err
		}

		return syncOutboxAggregates(ctx, tx, uniqueIDs)
	}); err != nil {
		return fmt.Errorf("project outbox aggregate transaction: %w", err)
	}

	return nil
}

func lockOutboxAggregateParents(ctx context.Context, tx dbx.Querier, outboxIDs []int64) error {
	if len(outboxIDs) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, deliveryRepositoryAggregateLockSQL, outboxIDs); err != nil {
		return fmt.Errorf("lock outbox aggregate parents: %w", err)
	}

	return nil
}

func syncOutboxAggregates(ctx context.Context, tx dbx.Querier, outboxIDs []int64) error {
	if len(outboxIDs) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, deliveryRepositoryAggregateSyncSQL,
		outboxIDs,
		domain.OutboxStatusPending,
		DeliveryStatusSending,
		domain.OutboxStatusFailed,
		DeliveryStatusQuarantined,
		domain.OutboxStatusSent,
		outboxAggregateFailedErrorText,
	); err != nil {
		return fmt.Errorf("update outbox aggregate statuses: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) FindPendingOutboxIDsForAggregateSync(ctx context.Context, batchSize int) ([]int64, error) {
	if r == nil || r.db == nil || batchSize <= 0 {
		return nil, nil
	}

	var outboxIDs []int64

	if err := dbx.SelectSQL(ctx, r.db, &outboxIDs, "find pending outbox ids for aggregate sync", findPendingOutboxIDsForAggregateSyncSQL, batchSize); err != nil {
		return nil, fmt.Errorf("find pending outbox ids for aggregate sync: %w", err)
	}

	return outboxIDs, nil
}
