package migrationrunner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"

	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

const AdvisoryLockKey int64 = 0x484F4C4F41504901

var createIndexConcurrentlyPattern = regexp.MustCompile(mustPattern("create_index_concurrently.re"))

type Config struct {
	BaselineThrough string
	LockKey         int64
	Logf            func(format string, args ...any)
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

	exec := &guardedExecer{pool: pool}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire lock connection: %w", err)
	}
	defer conn.Release()

	lockSession := pgxAdvisoryLockSession{conn: conn}
	var result Result
	err = dbmigrate.WithAdvisoryLock(ctx, lockSession, dbmigrate.LockConfig{Key: lockKey(cfg)}, func(lockCtx context.Context) error {
		var applyErr error
		result, applyErr = applyLocked(lockCtx, pool, fsys, exec, cfg)
		return applyErr
	})
	if err != nil {
		return Result{}, err
	}

	return result, nil
}

func applyLocked(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, exec *guardedExecer, cfg Config) (Result, error) {
	ledger := dbmigrate.Ledger{}
	if err := ledger.Ensure(ctx, exec.Exec); err != nil {
		return Result{}, err
	}

	entries, err := dbmigrate.Manifest(fsys)
	if err != nil {
		return Result{}, fmt.Errorf("dbmigrate: %w", err)
	}

	if err := reconcileBaseline(ctx, pool, fsys, exec, ledger, entries, cfg); err != nil {
		return Result{}, err
	}

	result, err := applyManifest(ctx, pool, fsys, exec, ledger, entries, cfg)
	if err != nil {
		return Result{}, err
	}

	if err := assertNoInvalidIndexes(ctx, pool); err != nil {
		return Result{}, err
	}
	return result, nil
}

func applyManifest(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, exec *guardedExecer, ledger dbmigrate.Ledger, entries []string, cfg Config) (Result, error) {
	querier := pgxRowQuerier{pool: pool}
	result := Result{Total: len(entries)}
	for _, name := range entries {
		alreadyApplied, appliedErr := ledger.Applied(ctx, querier, name)
		if appliedErr != nil {
			return Result{}, appliedErr
		}
		if alreadyApplied {
			cfg.logf("skip %s (already applied)", name)
			result.Skipped++
			continue
		}

		cfg.logf("apply %s", name)
		if err := applyEntry(ctx, fsys, exec, ledger, name); err != nil {
			return Result{}, err
		}
		result.Applied++
	}
	return result, nil
}

func applyEntry(ctx context.Context, fsys fs.FS, exec *guardedExecer, ledger dbmigrate.Ledger, name string) error {
	content, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	if err := exec.execFile(ctx, name, string(content)); err != nil {
		return err
	}
	return ledger.Record(ctx, exec.Exec, name)
}

// reconcileBaseline은 apply-all.sh의 ledger 결정 블록을 포팅한다. 핵심 제약: 기존
// 스키마 + 빈 ledger + watermark 미지정이면 전체 manifest를 applied로 stamp해 아직
// 미적용인 마이그레이션이 조용히 skip되는 사고(073 DB에 074-082 유실)가 나므로 거부한다.
func reconcileBaseline(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, exec *guardedExecer, ledger dbmigrate.Ledger, entries []string, cfg Config) error {
	count, err := ledgerCount(ctx, pool)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	baseSchema, err := baseSchemaPresent(ctx, pool)
	if err != nil {
		return err
	}
	if !baseSchema {
		return nil
	}

	through := strings.TrimSpace(cfg.BaselineThrough)
	if through == "" {
		return errors.New(
			"existing schema detected with an empty schema_migrations ledger; " +
				"refusing to stamp the whole manifest as applied (that would silently skip genuinely-pending migrations). " +
				"set MIGRATION_BASELINE_THROUGH to the last manifest migration already applied to this database, then rerun")
	}
	if !containsEntry(entries, through) {
		return fmt.Errorf("MIGRATION_BASELINE_THROUGH=%q is not a manifest migration filename", through)
	}

	cfg.logf("existing schema with empty ledger; baselining through %s (no SQL re-run), applying the remainder", through)
	if err := dbmigrate.Baseline(ctx, fsys, exec.Exec, through, ledger); err != nil {
		return fmt.Errorf("baseline migrations: %w", err)
	}
	return nil
}

func containsEntry(entries []string, target string) bool {
	return slices.Contains(entries, target)
}

func ledgerCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	if err := pool.QueryRow(ctx, mustSQL("ledger_count.sql")).Scan(&count); err != nil {
		return 0, fmt.Errorf("count schema_migrations: %w", err)
	}
	return count, nil
}

func baseSchemaPresent(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var present bool
	query := mustSQL("base_schema_present.sql")
	if err := pool.QueryRow(ctx, query).Scan(&present); err != nil {
		return false, fmt.Errorf("detect base schema: %w", err)
	}
	return present, nil
}

func lockKey(cfg Config) int64 {
	if cfg.LockKey != 0 {
		return cfg.LockKey
	}
	return AdvisoryLockKey
}

type guardedExecer struct {
	pool *pgxpool.Pool
}

func (e *guardedExecer) Exec(ctx context.Context, query string) error {
	_, err := e.pool.Exec(ctx, query)
	return err
}

// CONCURRENTLY 실패가 남긴 invalid index는 이름을 점유해 다음 실행의 IF NOT EXISTS를
// no-op으로 만들고, 그 no-op이 ledger에 applied로 굳으면 재빌드 경로가 사라진다. 따라서
// ledger 기록(호출자) 전에 감지·DROP하고 에러로 실패시켜 재실행이 같은 파일을 다시 적용하게 한다.
func (e *guardedExecer) execFile(ctx context.Context, name, content string) error {
	for _, stmt := range sqlsplit.Statements(content) {
		if _, err := e.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec %s: %w", name, err)
		}
	}
	if createIndexConcurrentlyPattern.MatchString(content) {
		return dropInvalidIndexes(ctx, e.pool)
	}
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
		if _, dropErr := pool.Exec(ctx, fmt.Sprintf(mustSQL("drop_invalid_index.sql.tpl"), name)); dropErr != nil {
			errs = append(errs, fmt.Errorf("drop invalid index %s: %w", name, dropErr))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid index cleanup: %w", errors.Join(errs...))
	}
	return fmt.Errorf("invalid PostgreSQL indexes dropped: %s", strings.Join(indexes, ", "))
}

func invalidIndexes(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, mustSQL("invalid_indexes.sql"))
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
	err := s.conn.QueryRow(ctx, mustSQL("try_advisory_lock.sql"), key).Scan(&acquired)
	return acquired, err
}

func (s pgxAdvisoryLockSession) AdvisoryUnlock(ctx context.Context, key int64) (bool, error) {
	var released bool
	err := s.conn.QueryRow(ctx, mustSQL("advisory_unlock.sql"), key).Scan(&released)
	return released, err
}
