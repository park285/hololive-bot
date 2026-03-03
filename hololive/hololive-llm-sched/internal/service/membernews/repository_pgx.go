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
	if q == nil || q.pool == nil {
		return fmt.Errorf("membernews pgx pool is nil")
	}
	_, err := q.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("pgx exec: %w", err)
	}
	return nil
}

func (q *pgxMemberNewsQuerier) Query(ctx context.Context, sql string, args ...any) (rowsScanner, error) {
	if q == nil || q.pool == nil {
		return nil, fmt.Errorf("membernews pgx pool is nil")
	}
	rows, err := q.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("pgx query: %w", err)
	}
	return rows, nil
}

func (q *pgxMemberNewsQuerier) QueryRow(ctx context.Context, sql string, args ...any) rowScanner {
	if q == nil || q.pool == nil {
		return nilRowScanner{err: fmt.Errorf("membernews pgx pool is nil")}
	}
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
