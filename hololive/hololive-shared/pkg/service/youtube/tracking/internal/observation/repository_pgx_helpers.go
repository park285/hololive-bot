package observation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
)

func execSQL(ctx context.Context, db trackingDB, action string, query string, args ...any) (int64, error) {
	return dbx.ExecSQL(ctx, db, action, query, args...)
}

func selectSQL(ctx context.Context, db trackingDB, dest any, action string, query string, args ...any) error {
	return dbx.SelectSQL(ctx, db, dest, action, query, args...)
}

func getSQL(ctx context.Context, db trackingDB, dest any, action string, query string, args ...any) (bool, error) {
	return dbx.GetSQL(ctx, db, dest, action, query, args...)
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

func inPgxTx(ctx context.Context, db trackingDB, fn func(tx trackingDB) error) error {
	if _, ok := db.(pgx.Tx); ok {
		return fn(db)
	}

	beginner, ok := db.(trackingTxBeginner)
	if !ok {
		return fmt.Errorf("db does not support transactions")
	}

	tx, err := beginner.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	return finishPgxTx(ctx, tx, fn(tx))
}

func finishPgxTx(ctx context.Context, tx pgx.Tx, fnErr error) error {
	if fnErr != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("transaction failed and rollback failed: %w", errors.Join(fnErr, rollbackErr))
		}
		return fnErr
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
