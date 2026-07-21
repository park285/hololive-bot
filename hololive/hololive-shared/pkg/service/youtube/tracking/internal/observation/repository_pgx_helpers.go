package observation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/pgxutil"
)

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
			rollbackErr := pgxutil.Rollback(ctx, tx)
			if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				slog.Default().Warn("pgx tracking transaction rollback after panic failed", slog.Any("error", rollbackErr))
			}
			panic(p)
		}
	}()
	return finishPgxTx(ctx, tx, fn(tx))
}

func finishPgxTx(ctx context.Context, tx pgx.Tx, fnErr error) error {
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
