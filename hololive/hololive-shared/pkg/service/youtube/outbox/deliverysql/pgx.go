package deliverysql

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/pgxutil"
)

type DeliveryDB interface {
	dbx.Querier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func IsNilDB(db any) bool {
	return reflectutil.IsNil(db)
}

func AsQuerier(db any) dbx.Querier {
	if IsNilDB(db) {
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

func AppendDeliveryInt64Args(args []any, values []int64) []any {
	for _, value := range values {
		args = append(args, value)
	}

	return args
}

func AppendDeliveryStringArgs(args []any, values []string) []any {
	for _, value := range values {
		args = append(args, value)
	}

	return args
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

func InDeliveryTx(ctx context.Context, db DeliveryDB, fn func(tx dbx.Querier) error) error {
	if db == nil {
		return errors.New("db is nil")
	}

	if fn == nil {
		return nil
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer rollbackDeliveryTxOnPanic(ctx, tx)

	if err := finishDeliveryTx(ctx, tx, fn(tx)); err != nil {
		return fmt.Errorf("finish delivery tx: %w", err)
	}

	return nil
}

func rollbackDeliveryTxOnPanic(ctx context.Context, tx pgx.Tx) {
	if p := recover(); p != nil {
		rollbackErr := pgxutil.Rollback(ctx, tx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Default().Warn("delivery transaction rollback after panic failed", slog.Any("error", rollbackErr))
		}

		panic(p)
	}
}

func finishDeliveryTx(ctx context.Context, tx pgx.Tx, fnErr error) error {
	if fnErr != nil {
		if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil {
			return fmt.Errorf("transaction failed and rollback failed: %w", errors.Join(fnErr, rollbackErr))
		}

		return fnErr
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func PostgresPlaceholders(query string) string {
	return dbx.PostgresPlaceholders(query)
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
