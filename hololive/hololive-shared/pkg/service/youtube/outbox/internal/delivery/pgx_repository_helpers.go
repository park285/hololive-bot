package delivery

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type deliveryDB interface {
	dbx.Querier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func isNilDB(db any) bool {
	if db == nil {
		return true
	}
	value := reflect.ValueOf(db)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func execDeliverySQL(ctx context.Context, db dbx.Querier, action string, query string, args ...any) (int64, error) {
	tag, err := db.Exec(ctx, postgresPlaceholders(query), args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", action, err)
	}
	return tag.RowsAffected(), nil
}

func selectDeliverySQL(ctx context.Context, db dbx.Querier, dest any, action string, query string, args ...any) error {
	if err := pgxscan.Select(ctx, db, dest, postgresPlaceholders(query), args...); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func getDeliverySQL(ctx context.Context, db dbx.Querier, dest any, action string, query string, args ...any) (bool, error) {
	err := pgxscan.Get(ctx, db, dest, postgresPlaceholders(query), args...)
	if err == nil {
		return true, nil
	}
	if pgxscan.NotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("%s: %w", action, err)
}

func deliveryInClause(column string, count int) string {
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

func appendDeliveryInt64Args(args []any, values []int64) []any {
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func appendDeliveryStringArgs(args []any, values []string) []any {
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func appendDeliveryOutboxKindArgs(args []any, values ...domain.OutboxKind) []any {
	for _, value := range values {
		args = append(args, string(value))
	}
	return args
}

func appendDeliveryOutboxStatusArgs(args []any, values ...domain.OutboxStatus) []any {
	for _, value := range values {
		args = append(args, string(value))
	}
	return args
}

func appendDeliveryAlarmTypeArgs(args []any, values ...domain.AlarmType) []any {
	for _, value := range values {
		args = append(args, string(value))
	}
	return args
}

func inDeliveryTx(ctx context.Context, db deliveryDB, fn func(tx dbx.Querier) error) error {
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
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("transaction failed and rollback failed: %w", errors.Join(err, rollbackErr))
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func postgresPlaceholders(query string) string {
	var out strings.Builder
	index := 1
	for i := 0; i < len(query); i++ {
		if query[i] != '?' {
			out.WriteByte(query[i])
			continue
		}
		out.WriteString(fmt.Sprintf("$%d", index))
		index++
	}
	return out.String()
}

func scanOutboxRow(row pgx.CollectableRow) (domain.YouTubeNotificationOutbox, error) {
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
	return item, err
}

func scanDeliveryRow(row pgx.CollectableRow) (domain.YouTubeNotificationDelivery, error) {
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
	return item, err
}
