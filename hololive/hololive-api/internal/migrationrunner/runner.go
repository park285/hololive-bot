package migrationrunner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"
)

const AdvisoryLockKey int64 = 0x484F4C4F41504901

var createIndexConcurrentlyPattern = regexp.MustCompile(`(?is)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\b`)

type Config struct {
	BaselineThrough string
	LockKey         int64
}

type Result struct {
	Applied int
}

func Run(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, cfg Config) (Result, error) {
	if pool == nil {
		return Result{}, fmt.Errorf("postgres pool is nil")
	}
	if fsys == nil {
		return Result{}, fmt.Errorf("migration fs is nil")
	}

	exec, err := newGuardedExecer(pool, fsys)
	if err != nil {
		return Result{}, err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire lock connection: %w", err)
	}
	defer conn.Release()

	lockSession := pgxAdvisoryLockSession{conn: conn}
	err = dbmigrate.WithAdvisoryLock(ctx, lockSession, dbmigrate.LockConfig{Key: lockKey(cfg)}, func(lockCtx context.Context) error {
		return applyLocked(lockCtx, pool, fsys, exec, cfg)
	})
	if err != nil {
		return Result{}, err
	}

	return Result{Applied: exec.applied}, nil
}

func applyLocked(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, exec *guardedExecer, cfg Config) error {
	ledger := dbmigrate.Ledger{}
	if err := ledger.Ensure(ctx, exec.Exec); err != nil {
		return err
	}

	baselineThrough := strings.TrimSpace(cfg.BaselineThrough)
	if baselineThrough != "" {
		if err := dbmigrate.Baseline(ctx, fsys, exec.Exec, baselineThrough, ledger); err != nil {
			return fmt.Errorf("baseline migrations: %w", err)
		}
	}

	if err := dbmigrate.Apply(ctx, fsys, exec.Exec, dbmigrate.WithLedger(ledger, pgxRowQuerier{pool: pool})); err != nil {
		return err
	}
	return assertNoInvalidIndexes(ctx, pool)
}

func lockKey(cfg Config) int64 {
	if cfg.LockKey != 0 {
		return cfg.LockKey
	}
	return AdvisoryLockKey
}

type guardedExecer struct {
	pool         *pgxpool.Pool
	migrationSQL map[string]bool
	applied      int
}

func newGuardedExecer(pool *pgxpool.Pool, fsys fs.FS) (*guardedExecer, error) {
	migrationSQL, err := migrationSQLByContent(fsys)
	if err != nil {
		return nil, err
	}
	return &guardedExecer{pool: pool, migrationSQL: migrationSQL}, nil
}

func migrationSQLByContent(fsys fs.FS) (map[string]bool, error) {
	entries, err := dbmigrate.Manifest(fsys)
	if err != nil {
		return nil, fmt.Errorf("dbmigrate: %w", err)
	}

	queries := make(map[string]bool, len(entries))
	for _, name := range entries {
		content, readErr := fs.ReadFile(fsys, name)
		if readErr != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, readErr)
		}
		queries[string(content)] = true
	}
	return queries, nil
}

func (e *guardedExecer) Exec(ctx context.Context, query string) error {
	if _, err := e.pool.Exec(ctx, query); err != nil {
		return err
	}

	if !e.migrationSQL[query] {
		return nil
	}
	if createIndexConcurrentlyPattern.MatchString(query) {
		if err := dropInvalidIndexes(ctx, e.pool); err != nil {
			return err
		}
	}
	e.applied++
	return nil
}

func assertNoInvalidIndexes(ctx context.Context, pool *pgxpool.Pool) error {
	indexes, err := invalidIndexes(ctx, pool)
	if err != nil {
		return err
	}
	if len(indexes) > 0 {
		return fmt.Errorf("invalid PostgreSQL indexes remain: %s", strings.Join(indexes, ", "))
	}
	return nil
}

func dropInvalidIndexes(ctx context.Context, pool *pgxpool.Pool) error {
	indexes, err := invalidIndexes(ctx, pool)
	if err != nil {
		return err
	}
	if len(indexes) == 0 {
		return nil
	}

	errs := make([]error, 0, len(indexes))
	for _, name := range indexes {
		if _, dropErr := pool.Exec(ctx, "DROP INDEX IF EXISTS "+name); dropErr != nil {
			errs = append(errs, fmt.Errorf("drop invalid index %s: %w", name, dropErr))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid index cleanup: %w", errors.Join(errs...))
	}
	return fmt.Errorf("invalid PostgreSQL indexes dropped: %s", strings.Join(indexes, ", "))
}

func invalidIndexes(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT format('%I.%I', n.nspname, c.relname)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE NOT i.indisvalid
		ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("query invalid indexes: %w", err)
	}
	defer rows.Close()

	var indexes []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			return nil, fmt.Errorf("scan invalid index: %w", scanErr)
		}
		indexes = append(indexes, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read invalid indexes: %w", err)
	}
	return indexes, nil
}

type pgxRowQuerier struct {
	pool *pgxpool.Pool
}

func (q pgxRowQuerier) QueryRow(ctx context.Context, query string, args ...any) dbmigrate.Row {
	return q.pool.QueryRow(ctx, query, args...)
}

type pgxAdvisoryLockSession struct {
	conn *pgxpool.Conn
}

func (s pgxAdvisoryLockSession) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var acquired bool
	err := s.conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired)
	return acquired, err
}

func (s pgxAdvisoryLockSession) AdvisoryUnlock(ctx context.Context, key int64) (bool, error) {
	var released bool
	err := s.conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", key).Scan(&released)
	return released, err
}
