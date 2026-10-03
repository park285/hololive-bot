package deliverysql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type DeliveryDB interface {
	dbx.Querier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func AsQuerier(db any) dbx.Querier {
	if reflectutil.IsNil(db) {
		return nil
	}

	if typed, ok := db.(dbx.Querier); ok {
		return typed
	}

	return nil
}

func DeliveryInClause(column string, count int) string {
	if count <= 0 {
		return "FALSE"
	}

	return column + " IN (" + inDeliveryPlaceholders(count) + ")"
}

func inDeliveryPlaceholders(count int) string {
	if count <= 0 {
		return "NULL"
	}

	return strings.TrimSuffix(strings.Repeat("?, ", count), ", ")
}

func AppendDeliveryOutboxKindArgs(args []any, values ...domain.OutboxKind) []any {
	for _, value := range values {
		args = append(args, string(value))
	}

	return args
}

func AppendDeliveryOutboxStatusArgs(args []any, values ...domain.OutboxStatus) []any {
	for _, value := range values {
		args = append(args, string(value))
	}

	return args
}

func ScanOutboxRow(row pgx.CollectableRow) (domain.YouTubeNotificationOutbox, error) {
	var item domain.YouTubeNotificationOutbox

	err := row.Scan(
		&item.ID,
		&item.Kind,
		&item.ChannelID,
		&item.ContentID,
		&item.Payload,
		&item.Status,
		&item.AttemptCount,
		&item.NextAttemptAt,
		&item.CreatedAt,
		&item.LockedAt,
		&item.SentAt,
		&item.Error,
	)
	if err != nil {
		return item, fmt.Errorf("scan outbox row: %w", err)
	}

	return item, nil
}

func ScanDeliveryRow(row pgx.CollectableRow) (domain.YouTubeNotificationDelivery, error) {
	var item domain.YouTubeNotificationDelivery

	err := row.Scan(
		&item.ID,
		&item.OutboxID,
		&item.RoomID,
		&item.Status,
		&item.AttemptCount,
		&item.NextAttemptAt,
		&item.CreatedAt,
		&item.LockedAt,
		&item.SentAt,
		&item.Error,
	)
	if err != nil {
		return item, fmt.Errorf("scan delivery row: %w", err)
	}

	return item, nil
}

const deleteBatchYield = 10 * time.Millisecond

func YieldBetweenDeleteBatches(ctx context.Context) error {
	timer := time.NewTimer(deleteBatchYield)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("yield between delete batches: %w", err)
		}

		return nil
	case <-timer.C:
		return nil
	}
}
