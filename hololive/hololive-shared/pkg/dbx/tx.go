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

	"github.com/kapu/hololive-shared/pkg/pgxutil"
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

	if err := finishPgxTx(ctx, tx, fn(tx)); err != nil {
		return fmt.Errorf("finish pgx tx: %w", err)
	}

	return nil
}

func rollbackPgxTxOnPanic(ctx context.Context, tx Tx) {
	if p := recover(); p != nil {
		rollbackErr := pgxutil.Rollback(ctx, tx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.Default().Warn("pgx transaction rollback after panic failed", slog.Any("error", rollbackErr))
		}

		panic(p)
	}
}

// finishPgxTx는 fn 실행 결과에 따라 트랜잭션을 커밋하거나 롤백한다.
func finishPgxTx(ctx context.Context, tx Tx, fnErr error) error {
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
	if err != nil {
		return result, fmt.Errorf("in pgx tx: %w", err)
	}

	return result, nil
}

const maxHotpathBatchStatements = 128

// Statement는 같은 트랜잭션 안에서 순서대로 실행할 SQL이다.
// Operation은 오류 위치를 식별하는 고정 문자열이며 사용자 데이터나 인자를 넣지 않는다.
type Statement struct {
	SQL       string
	Args      []any
	Operation string
}

type statementBatchSender interface {
	SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
}

// ExecStatements는 호출자가 소유한 트랜잭션에서 입력 순서대로 SQL을 실행한다.
// pgx 트랜잭션이면 최대 128개씩 전송하고, 배치를 지원하지 않으면 순차 실행한다.
// 커밋/롤백은 호출자 책임이며, 오류 후에는 반드시 롤백해야 한다.
// 기존 테이블의 DML 전용이다. 선행 DDL에 의존하는 문장은 별도 Exec로 실행한다.
// 빈 입력은 I/O 없이 성공한다. 실행 결과는 다음 배치나 반환 전에 항상 닫는다.
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
		if _, err := tx.Exec(ctx, statement.SQL, statement.Args...); err != nil {
			return fmt.Errorf("execute statement %d (%s): %w", i, statement.Operation, err)
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
	// 정상 반환과 오류뿐 아니라 panic에서도 남은 응답을 회수한다.
	defer func() {
		if closeErr := results.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close statement batch at %d: %w", offset, closeErr))
		}
	}()
	for i := range statements {
		if _, executeErr := results.Exec(); executeErr != nil {
			return fmt.Errorf("execute statement %d (%s): %w", offset+i, statements[i].Operation, executeErr)
		}
	}
	return nil
}
