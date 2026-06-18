// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dbx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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
		return fmt.Errorf("pgx pool is nil")
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
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Default().Warn("pgx transaction rollback after panic failed", slog.Any("error", rollbackErr))
		}
		panic(p)
	}
}

// finishPgxTx는 fn 실행 결과에 따라 트랜잭션을 커밋하거나 롤백한다.
func finishPgxTx(ctx context.Context, tx Tx, fnErr error) error {
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

func InPgxTxWithResult[T any](ctx context.Context, pool *pgxpool.Pool, fn func(tx Tx) (T, error)) (T, error) {
	var result T
	if pool == nil {
		return result, fmt.Errorf("pgx pool is nil")
	}
	if fn == nil {
		return result, nil
	}

	err := InPgxTx(ctx, pool, func(tx Tx) error {
		var txErr error
		result, txErr = fn(tx)
		return txErr
	})
	if err != nil {
		return result, err
	}
	return result, nil
}
