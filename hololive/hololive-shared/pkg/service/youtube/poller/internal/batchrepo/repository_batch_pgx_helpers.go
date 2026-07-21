package batchrepo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/pgxutil"
)

type batchDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
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

	defer rollbackBatchTxOnPanic(ctx, tx)

	return finishBatchTx(ctx, tx, fn(tx))
}

func rollbackBatchTxOnPanic(ctx context.Context, tx pgx.Tx) {
	if p := recover(); p != nil {
		rollbackErr := pgxutil.Rollback(ctx, tx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Default().Warn("pgx batch transaction rollback after panic failed", slog.Any("error", rollbackErr))
		}
		panic(p)
	}
}

func finishBatchTx(ctx context.Context, tx pgx.Tx, fnErr error) error {
	if fnErr != nil {
		if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil {
			return fmt.Errorf("pgx transaction failed and rollback failed: %w", errors.Join(fnErr, rollbackErr))
		}
		return fmt.Errorf("pgx transaction failed: %w", fnErr)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pgx transaction: %w", err)
	}
	return nil
}
