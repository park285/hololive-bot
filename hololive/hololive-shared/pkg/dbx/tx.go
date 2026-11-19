package dbx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/postgres/pgxdb"
)

type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Tx interface {
	Querier
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

func InPgxTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx Tx) error) error {
	if pool == nil {
		return errors.New("pgx pool is nil")
	}

	if fn == nil {
		return nil
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin pgx transaction: %w", err)
	}

	defer rollbackPgxTxOnPanic(ctx, tx)

	return finishPgxTx(ctx, tx, fn(tx))
}

func rollbackPgxTxOnPanic(ctx context.Context, tx Tx) {
	if p := recover(); p != nil {
		rollbackErr := pgxdb.Rollback(ctx, tx)
		if rollbackErr != nil {
			slog.Default().Warn("pgx transaction rollback after panic failed", slog.Any("error", rollbackErr))
		}

		panic(p)
	}
}

func finishPgxTx(ctx context.Context, tx Tx, fnErr error) error {
	if fnErr != nil {
		if rollbackErr := pgxdb.Rollback(ctx, tx); rollbackErr != nil {
			return fmt.Errorf("run pgx transaction and roll back: %w", errors.Join(fnErr, rollbackErr))
		}

		return fmt.Errorf("run pgx transaction: %w", fnErr)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pgx transaction: %w", err)
	}

	return nil
}

func InPgxTxWithResult[T any](ctx context.Context, pool *pgxpool.Pool, fn func(tx Tx) (T, error)) (T, error) {
	var result T

	if pool == nil {
		return result, errors.New("pgx pool is nil")
	}

	if fn == nil {
		return result, nil
	}

	err := InPgxTx(ctx, pool, func(tx Tx) error {
		var txErr error

		result, txErr = fn(tx)

		return txErr
	})

	return result, err
}

const maxHotpathBatchStatements = 128

type Statement struct {
	SQL        string
	Args       []any
	Operation  string
	RequireRow bool
}

type NoRowsError struct {
	Operation string
	Index     int
}

func (e *NoRowsError) Error() string {
	return fmt.Sprintf("execute statement %d (%s): no rows affected", e.Index, e.Operation)
}

func requireStatementRow(statement *Statement, index int, tag pgconn.CommandTag) error {
	if statement.RequireRow && tag.RowsAffected() == 0 {
		return &NoRowsError{Index: index, Operation: statement.Operation}
	}

	return nil
}

type statementBatchSender interface {
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
}

func ExecStatements(ctx context.Context, tx Tx, statements []Statement) error {
	if len(statements) == 0 {
		return nil
	}

	if tx == nil {
		return errors.New("execute statements: transaction is nil")
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("execute statements: %w", err)
	}

	if sender, ok := tx.(statementBatchSender); ok {
		return execStatementBatches(ctx, sender, statements)
	}

	for i := range statements {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("execute statement %d: %w", i, err)
		}

		statement := &statements[i]

		tag, err := tx.Exec(ctx, statement.SQL, statement.Args...)
		if err != nil {
			return fmt.Errorf("execute statement %d (%s): %w", i, statement.Operation, err)
		}

		if err := requireStatementRow(statement, i, tag); err != nil {
			return err
		}
	}

	return nil
}

func execStatementBatches(ctx context.Context, sender statementBatchSender, statements []Statement) error {
	for start := 0; start < len(statements); start += maxHotpathBatchStatements {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("execute statement batch at %d: %w", start, err)
		}

		end := min(start+maxHotpathBatchStatements, len(statements))
		if err := execStatementBatch(ctx, sender, statements[start:end], start); err != nil {
			return fmt.Errorf("execute statement batch: %w", err)
		}
	}

	return nil
}

func execStatementBatch(ctx context.Context, sender statementBatchSender, statements []Statement, offset int) (err error) {
	batch := &pgx.Batch{}

	for i := range statements {
		batch.Queue(statements[i].SQL, statements[i].Args...)
	}

	results := sender.SendBatch(ctx, batch)
	if results == nil {
		return errors.New("execute statement batch: results are nil")
	}

	defer func() {
		if closeErr := results.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close statement batch at %d: %w", offset, closeErr))
		}
	}()

	for i := range statements {
		tag, executeErr := results.Exec()
		if executeErr != nil {
			return fmt.Errorf("execute statement %d (%s): %w", offset+i, statements[i].Operation, executeErr)
		}

		if rowErr := requireStatementRow(&statements[i], offset+i, tag); rowErr != nil {
			return rowErr
		}
	}

	return nil
}
