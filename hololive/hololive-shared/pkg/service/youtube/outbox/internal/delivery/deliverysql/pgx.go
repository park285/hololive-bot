package deliverysql

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/pgxutil"
)

type DeliveryDB interface {
	dbx.Querier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func IsNilDB(db any) bool {
	if db == nil {
		return true
	}
	value := reflect.ValueOf(db)
	kind := value.Kind()
	if kind == reflect.Chan ||
		kind == reflect.Func ||
		kind == reflect.Interface ||
		kind == reflect.Map ||
		kind == reflect.Pointer ||
		kind == reflect.Slice {
		return value.IsNil()
	}
	return false
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

func ExecDeliverySQL(ctx context.Context, db dbx.Querier, action, query string, args ...any) (int64, error) {
	tag, err := db.Exec(ctx, PostgresPlaceholders(query), args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", action, err)
	}
	return tag.RowsAffected(), nil
}

func SelectDeliverySQL(ctx context.Context, db dbx.Querier, dest any, action, query string, args ...any) error {
	if err := pgxscan.Select(ctx, db, dest, PostgresPlaceholders(query), args...); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func GetDeliverySQL(ctx context.Context, db dbx.Querier, dest any, action, query string, args ...any) (bool, error) {
	err := pgxscan.Get(ctx, db, dest, PostgresPlaceholders(query), args...)
	if err == nil {
		return true, nil
	}
	if pgxscan.NotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("%s: %w", action, err)
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

func AppendDeliveryAlarmTypeArgs(args []any, values ...domain.AlarmType) []any {
	for _, value := range values {
		args = append(args, string(value))
	}
	return args
}

func InDeliveryTx(ctx context.Context, db DeliveryDB, fn func(tx dbx.Querier) error) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if fn == nil {
		return nil
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer rollbackDeliveryTxOnPanic(ctx, tx)
	return finishDeliveryTx(ctx, tx, fn(tx))
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
	var out strings.Builder
	index := 1
	for i := range len(query) {
		if query[i] != '?' {
			out.WriteByte(query[i])
			continue
		}
		fmt.Fprintf(&out, "$%d", index)
		index++
	}
	return out.String()
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
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
