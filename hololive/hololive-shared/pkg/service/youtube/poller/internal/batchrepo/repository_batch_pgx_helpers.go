package batchrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

	defer func() {
		if p := recover(); p != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				panic(fmt.Errorf("panic during pgx transaction and rollback failed: %w", errors.Join(fmt.Errorf("%v", p), rollbackErr)))
			}
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
