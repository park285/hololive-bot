package migrationrunner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"

	"github.com/kapu/hololive-shared/pkg/pgxutil"
	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

const AdvisoryLockKey int64 = 0x484F4C4F41504901

const (
	sessionLockTimeout      = 10 * time.Second
	sessionStatementTimeout = 4 * time.Minute
	advisoryUnlockTimeout   = 5 * time.Second
)

type Config struct {
	BaselineThrough        string
	LockKey                int64
	AllowBlockingIndexDrop bool
	Logf                   func(format string, args ...any)
}

type Result struct {
	Applied int
	Skipped int
	Total   int
}

func (c Config) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func Run(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, cfg Config) (Result, error) {
	if pool == nil {
		return Result{}, fmt.Errorf("postgres pool is nil")
	}
	if fsys == nil {
		return Result{}, fmt.Errorf("migration fs is nil")
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	exec := &guardedExecer{conn: conn}
	sessionCfg := dbmigrate.SessionConfig{
		LockTimeout:      sessionLockTimeout,
		StatementTimeout: sessionStatementTimeout,
	}
	if err := sessionCfg.Configure(ctx, exec.Exec); err != nil {
		return Result{}, fmt.Errorf("configure migration session: %w", err)
	}

	lockSession := pgxAdvisoryLockSession{conn: conn}
	var result Result
	err = dbmigrate.WithAdvisoryLock(ctx, lockSession, dbmigrate.LockConfig{Key: lockKey(cfg), Release: advisoryUnlockTimeout}, func(lockCtx context.Context) error {
		var applyErr error
		result, applyErr = applyLocked(lockCtx, conn, fsys, exec, cfg)
		return applyErr
	})
	if err != nil {
		return Result{}, err
	}

	return result, nil
}

func applyLocked(ctx context.Context, conn *pgxpool.Conn, fsys fs.FS, exec *guardedExecer, cfg Config) (Result, error) {
	ledger := dbmigrate.Ledger{}
	if err := ledger.Ensure(ctx, exec.Exec); err != nil {
		return Result{}, err
	}
	if err := ensureChecksumTable(ctx, exec.Exec); err != nil {
		return Result{}, err
	}

	entries, err := dbmigrate.Manifest(fsys)
	if err != nil {
		return Result{}, fmt.Errorf("dbmigrate: %w", err)
	}

	if err := reconcileBaseline(ctx, conn, fsys, ledger, entries, cfg); err != nil {
		return Result{}, err
	}
	allowBaselineChecksumBackfill, err := guardEpochResidue(ctx, conn, ledger, entries)
	if err != nil {
		return Result{}, err
	}
	if err := configureBlockingIndexDropPolicy(ctx, conn, exec, cfg); err != nil {
		return Result{}, err
	}

	result, err := applyManifest(ctx, conn, fsys, exec, ledger, entries, allowBaselineChecksumBackfill, cfg)
	if err != nil {
		return Result{}, err
	}

	if err := assertNoInvalidIndexes(ctx, conn); err != nil {
		return Result{}, err
	}
	return result, nil
}

func lockKey(cfg Config) int64 {
	if cfg.LockKey != 0 {
		return cfg.LockKey
	}
	return AdvisoryLockKey
}

// session timeout(set_config)이 conn scope라 pool 실행으로 되돌리면 무효화된다 —
// 모든 실행은 advisory lock을 쥔 pinned conn 하나에서 돌아야 한다.
type guardedExecer struct {
	conn                   *pgxpool.Conn
	allowBlockingIndexDrop bool
}

func (e *guardedExecer) Exec(ctx context.Context, query string, args ...any) error {
	_, err := e.conn.Exec(ctx, query, args...)
	return err
}

// CONCURRENTLY 실패가 남긴 invalid index는 이름을 점유해 다음 실행의 IF NOT EXISTS를
// no-op으로 만들 수 있다. 다른 운영 작업의 index를 건드리지 않도록 현재 파일의 대상만 정리한다.
func (e *guardedExecer) execSegment(ctx context.Context, name string, segment sqlsplit.Segment) error {
	if segment.Transactional {
		return e.execTxSegment(ctx, name, segment.Statements)
	}
	for _, stmt := range segment.Statements {
		if _, err := e.conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec %s: %w", name, err)
		}
	}
	return nil
}

func (e *guardedExecer) execTxSegment(ctx context.Context, name string, statements []string) error {
	tx, err := e.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("exec %s: begin: %w", name, err)
	}
	if err := execTxStatements(ctx, tx, name, statements); err != nil {
		return rollbackTxSegmentOnError(ctx, tx, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("exec %s: commit: %w", name, err)
	}
	return nil
}

func execTxStatements(ctx context.Context, tx pgx.Tx, name string, statements []string) error {
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec %s: %w", name, err)
		}
	}
	return nil
}

func rollbackTxSegmentOnError(ctx context.Context, tx pgx.Tx, err error) error {
	if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
		return errors.Join(err, fmt.Errorf("rollback tx segment: %w", rollbackErr))
	}
	return err
}

func assertNoInvalidIndexes(ctx context.Context, conn *pgxpool.Conn) error {
	indexes, err := invalidIndexes(ctx, conn, nil)
	if err != nil {
		return err
	}
	if len(indexes) > 0 {
		return fmt.Errorf("invalid PostgreSQL indexes remain: %s", strings.Join(indexes, ", "))
	}
	return nil
}

func dropInvalidIndexes(ctx context.Context, conn *pgxpool.Conn, targets []concurrentIndexTarget) error {
	if len(targets) == 0 {
		return nil
	}
	indexes, err := invalidIndexes(ctx, conn, targets)
	if err != nil {
		return err
	}
	if len(indexes) == 0 {
		return nil
	}

	errs := make([]error, 0, len(indexes))
	for _, name := range indexes {
		if _, dropErr := conn.Exec(ctx, fmt.Sprintf(mustSQL("drop_invalid_index.sql.tpl"), name)); dropErr != nil {
			errs = append(errs, fmt.Errorf("drop invalid index %s: %w", name, dropErr))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid index cleanup: %w", errors.Join(errs...))
	}
	return fmt.Errorf("invalid PostgreSQL indexes dropped: %s", strings.Join(indexes, ", "))
}

func invalidIndexes(ctx context.Context, conn *pgxpool.Conn, targets []concurrentIndexTarget) ([]string, error) {
	query := mustSQL("invalid_indexes.sql")
	var args []any
	if len(targets) > 0 {
		query = mustSQL("invalid_named_indexes.sql")
		indexNames := make([]string, 0, len(targets))
		tableRelations := make([]string, 0, len(targets))
		for _, target := range targets {
			indexNames = append(indexNames, target.indexName)
			tableRelations = append(tableRelations, target.tableRelation)
		}
		args = append(args, indexNames, tableRelations)
	}
	rows, err := conn.Query(ctx, query, args...)
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
	conn *pgxpool.Conn
}

func (q pgxRowQuerier) QueryRow(ctx context.Context, query string, args ...any) dbmigrate.Row {
	return q.conn.QueryRow(ctx, query, args...)
}

type pgxAdvisoryLockSession struct {
	conn *pgxpool.Conn
}

func (s pgxAdvisoryLockSession) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var acquired bool
	err := s.conn.QueryRow(ctx, mustSQL("try_advisory_lock.sql"), key).Scan(&acquired)
	return acquired, err
}

func (s pgxAdvisoryLockSession) AdvisoryUnlock(ctx context.Context, key int64) (bool, error) {
	var released bool
	err := s.conn.QueryRow(ctx, mustSQL("advisory_unlock.sql"), key).Scan(&released)
	return released, err
}
