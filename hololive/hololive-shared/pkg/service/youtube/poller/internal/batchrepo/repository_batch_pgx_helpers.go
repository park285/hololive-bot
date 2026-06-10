package batchrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/internal/dbx"
)

type batchDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func execSQL(ctx context.Context, db batchDB, action string, query string, args ...any) (int64, error) {
	return dbx.ExecSQL(ctx, db, action, query, args...)
}

func selectSQL(ctx context.Context, db batchDB, dest any, action string, query string, args ...any) error {
	return dbx.SelectSQL(ctx, db, dest, action, query, args...)
}

func inPlaceholders(count int) string {
	return dbx.InPlaceholders(count)
}

func anyArgs[T any](values []T) []any {
	return dbx.AnyArgs(values)
}

func postgresPlaceholders(query string) string {
	return dbx.PostgresPlaceholders(query)
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
