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

package membernews

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type pgxMemberNewsQuerier struct {
	pool *pgxpool.Pool
}

func newPGXMemberNewsQuerier(pool *pgxpool.Pool) memberNewsQuerier {
	if pool == nil {
		return nil
	}
	return &pgxMemberNewsQuerier{pool: pool}
}

func (q *pgxMemberNewsQuerier) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := q.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("pgx exec: %w", err)
	}
	return nil
}

func (q *pgxMemberNewsQuerier) Query(ctx context.Context, sql string, args ...any) (rowsScanner, error) {
	rows, err := q.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("pgx query: %w", err)
	}
	return rows, nil
}

func (q *pgxMemberNewsQuerier) QueryRow(ctx context.Context, sql string, args ...any) rowScanner {
	return q.pool.QueryRow(ctx, sql, args...)
}

type nilRowScanner struct {
	err error
}

func (n nilRowScanner) Scan(_ ...any) error {
	if n.err != nil {
		return n.err
	}
	return fmt.Errorf("row scanner is nil")
}
