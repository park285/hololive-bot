package batchrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type batchDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func execSQL(ctx context.Context, db batchDB, action string, query string, args ...any) (int64, error) {
	tag, err := db.Exec(ctx, postgresPlaceholders(query), args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", action, err)
	}
	return tag.RowsAffected(), nil
}

func selectSQL(ctx context.Context, db batchDB, dest any, action string, query string, args ...any) error {
	if err := pgxscan.Select(ctx, db, dest, postgresPlaceholders(query), args...); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func inPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ", ")
}

func anyArgs[T any](values []T) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
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

func inBatchTx(ctx context.Context, db batchTxBeginner, fn func(tx batchDB) error) error {
	if db == nil {
		return fmt.Errorf("pgx db is nil")
	}
	if fn == nil {
		return nil
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin pgx transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	return finishBatchTx(ctx, tx, fn(tx))
}

func finishBatchTx(ctx context.Context, tx pgx.Tx, fnErr error) error {
	if fnErr != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("pgx transaction failed and rollback failed: %w", errors.Join(fnErr, rollbackErr))
		}
		return fmt.Errorf("pgx transaction failed: %w", fnErr)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pgx transaction: %w", err)
	}
	return nil
}
