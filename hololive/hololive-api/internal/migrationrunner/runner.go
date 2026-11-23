package migrationrunner

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"

	"github.com/kapu/hololive-shared/pkg/migrationsource"
)

const AdvisoryLockKey int64 = 0x484F4C4F41504901

const MaxStatementTimeout = 10 * time.Minute

const (
	sessionLockTimeout      = 10 * time.Second
	sessionStatementTimeout = 4 * time.Minute
	advisoryUnlockTimeout   = 5 * time.Second
)

type Config struct {
	MigrationWindow  bool
	VerifyOnly       bool
	InitializeOnly   bool
	StatementTimeout time.Duration
	BeforeInitialize func(context.Context) error
}

type Result struct {
	Applied int
	Skipped int
	Total   int
}

func Run(ctx context.Context, pool *pgxpool.Pool, source fs.FS, cfg Config) (Result, error) {
	if pool == nil {
		return Result{}, errors.New("postgres pool is nil")
	}

	if source == nil {
		return Result{}, errors.New("migration source is nil")
	}

	if cfg.StatementTimeout < 0 || cfg.StatementTimeout > MaxStatementTimeout {
		return Result{}, fmt.Errorf("statement timeout must be between 0 and %s", MaxStatementTimeout)
	}

	files, err := migrationsource.Load(source)
	if err != nil {
		return Result{}, fmt.Errorf("load migrations: %w", err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	exec := func(ctx context.Context, query string, args ...any) error {
		_, execErr := conn.Exec(ctx, query, args...)
		return execErr
	}
	sessionCfg := dbmigrate.SessionConfig{
		LockTimeout:      sessionLockTimeout,
		StatementTimeout: cmp.Or(cfg.StatementTimeout, sessionStatementTimeout),
	}

	if configureErr := sessionCfg.Configure(ctx, exec); configureErr != nil {
		return Result{}, fmt.Errorf("configure migration session: %w", configureErr)
	}

	lockSession := pgxAdvisoryLockSession{conn: conn}

	var result Result

	err = dbmigrate.WithAdvisoryLock(ctx, lockSession, dbmigrate.LockConfig{Key: AdvisoryLockKey, Release: advisoryUnlockTimeout}, func(lockCtx context.Context) error {
		var applyErr error

		result, applyErr = applyLocked(lockCtx, conn, files, cfg)
		if applyErr != nil {
			return fmt.Errorf("apply locked: %w", applyErr)
		}

		return nil
	})
	if err != nil {
		return Result{}, fmt.Errorf("with advisory lock: %w", err)
	}

	return result, nil
}

func applyLocked(ctx context.Context, conn *pgxpool.Conn, files []migrationsource.File, cfg Config) (Result, error) {
	var result Result

	opts := pgx.TxOptions{}

	if cfg.VerifyOnly {
		opts.AccessMode = pgx.ReadOnly
	}

	err := pgx.BeginTxFunc(ctx, conn, opts, func(tx pgx.Tx) error {
		if _, pathErr := tx.Exec(ctx, "SET LOCAL search_path = pg_catalog, public, pg_temp"); pathErr != nil {
			return fmt.Errorf("select migration namespace: %w", pathErr)
		}

		var migrations, checksums bool

		if err := tx.QueryRow(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL,
			to_regclass('public.schema_migration_checksums') IS NOT NULL`).Scan(&migrations, &checksums); err != nil {
			return fmt.Errorf("inspect migration ledgers: %w", err)
		}

		if migrations != checksums {
			return errors.New("incomplete migration ledger pair")
		}

		var applyErr error

		if migrations {
			result, applyErr = advance(ctx, tx, files, cfg)
		} else {
			result, applyErr = bootstrap(ctx, tx, files, cfg)
		}

		return applyErr
	})
	if err != nil {
		return Result{}, fmt.Errorf("resolve migration lineage: %w", err)
	}

	return result, nil
}

func advance(ctx context.Context, tx pgx.Tx, files []migrationsource.File, cfg Config) (Result, error) {
	epoch, applied, err := verifyHead(ctx, tx, files, cfg.MigrationWindow)
	if err != nil {
		return Result{}, err
	}

	pending := files[1+applied:]
	if (cfg.VerifyOnly || cfg.InitializeOnly) && len(pending) != 0 {
		return Result{}, fmt.Errorf("migration pending: %s", pending[0].Name)
	}

	if cfg.MigrationWindow && len(pending) != 0 {
		drained, err := openMigrationWindow(ctx, tx)
		if err != nil {
			return Result{}, err
		}

		if !drained {
			return Result{}, errors.New("pending migrations require the writer drain guard, its login gate and zero writer sessions")
		}
	}

	if err := applyFiles(ctx, tx, pending); err != nil {
		return Result{}, err
	}

	skipped := 1 + applied

	if epoch == 3 {
		skipped += 31
	}

	return Result{Skipped: skipped, Applied: len(pending), Total: skipped + len(pending)}, nil
}

func openMigrationWindow(ctx context.Context, tx pgx.Tx) (bool, error) {
	if _, err := tx.Exec(ctx, mustSQL("migration_window_marker.sql")); err != nil {
		return false, fmt.Errorf("publish migration window marker: %w", err)
	}

	var drained bool

	if err := tx.QueryRow(ctx, mustSQL("writer_drain_state.sql")).Scan(&drained); err != nil {
		return false, fmt.Errorf("inspect writer drain: %w", err)
	}

	return drained, nil
}

func bootstrap(ctx context.Context, tx pgx.Tx, files []migrationsource.File, cfg Config) (Result, error) {
	if cfg.MigrationWindow || cfg.VerifyOnly {
		return Result{}, errors.New("an initialized migration lineage is required; hold writers")
	}

	var empty bool

	if err := tx.QueryRow(ctx, mustSQL("empty_database.sql")).Scan(&empty); err != nil {
		return Result{}, fmt.Errorf("inspect empty database: %w", err)
	}

	if !empty {
		return Result{}, errors.New("untracked database objects; refusing initialization")
	}

	if cfg.BeforeInitialize != nil {
		if err := cfg.BeforeInitialize(ctx); err != nil {
			return Result{}, fmt.Errorf("prepare login roles: %w", err)
		}
	}

	if cfg.InitializeOnly {
		var ready bool

		if err := tx.QueryRow(ctx, mustSQL("initialization_roles.sql")).Scan(&ready); err != nil {
			return Result{}, fmt.Errorf("inspect application initialization prerequisites: %w", err)
		}

		if !ready {
			return Result{}, errors.New("initialize requires the provisioned migrator, application roles, default privileges and pg_stat_statements")
		}
	}

	if err := initialize(ctx, tx, files); err != nil {
		return Result{}, err
	}

	return Result{Applied: len(files), Total: len(files)}, nil
}

func verifyHead(ctx context.Context, tx pgx.Tx, files []migrationsource.File, migrationWindow bool) (int, int, error) {
	var epoch, applied, pending int

	if err := tx.QueryRow(ctx, mustSQL("lineage_head.sql"), migrationsource.Receipts(files)).Scan(&epoch, &applied, &pending); err != nil {
		return 0, 0, fmt.Errorf("inspect migration head and card schema (partial cutovers must stay held): %w", err)
	}

	if epoch == 0 {
		return 0, 0, errors.New("unrecognized or incomplete migration head; use the matching historical release and preserve the maintenance hold")
	}

	if err := files[applied].Verify(ctx, tx); err != nil {
		return 0, 0, fmt.Errorf("verify applied schema: %w", err)
	}

	if migrationWindow {
		var private bool

		if err := tx.QueryRow(ctx, mustSQL("migration_window_state.sql")).Scan(&private); err != nil {
			return 0, 0, fmt.Errorf("inspect protected migration endpoint: %w", err)
		}

		if !private {
			return 0, 0, errors.New("migration window requires the private migrator connection")
		}
	}

	return epoch, applied, nil
}

func initialize(ctx context.Context, tx pgx.Tx, files []migrationsource.File) error {
	exec := func(ctx context.Context, query string, args ...any) error {
		_, execErr := tx.Exec(ctx, query, args...)
		return execErr
	}
	ledger := dbmigrate.Ledger{Table: "public.schema_migrations"}

	if err := ledger.Ensure(ctx, exec); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}

	if _, err := tx.Exec(ctx, mustSQL("ensure_migration_checksums.sql")); err != nil {
		return fmt.Errorf("create checksum ledger: %w", err)
	}

	return applyFiles(ctx, tx, files)
}

func applyFiles(ctx context.Context, tx pgx.Tx, files []migrationsource.File) error {
	if len(files) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, `LOCK TABLE public.schema_migrations, public.schema_migration_checksums IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return fmt.Errorf("protect migration ledgers: %w", err)
	}

	for _, file := range files {
		before, err := receiptState(ctx, tx, "", "")
		if err != nil {
			return err
		}

		if applyErr := applyFile(ctx, tx, file); applyErr != nil {
			return applyErr
		}

		after, err := receiptState(ctx, tx, file.Name, file.Checksum)
		if err != nil {
			return err
		}

		if !bytes.Equal(before, after) {
			return fmt.Errorf("%s modified a ledger definition or an earlier receipt", file.Name)
		}
	}

	return nil
}

func receiptState(ctx context.Context, tx pgx.Tx, except, checksum string) ([]byte, error) {
	var digest []byte

	if err := tx.QueryRow(ctx, mustSQL("ledger_snapshot.sql"), except, checksum).Scan(&digest); err != nil {
		return nil, fmt.Errorf("inspect ledger definitions and preserved receipts: %w", err)
	}

	return digest, nil
}

func applyFile(ctx context.Context, tx pgx.Tx, file migrationsource.File) error {
	if err := file.Apply(ctx, tx); err != nil {
		return fmt.Errorf("execute pending migration: %w", err)
	}

	if _, err := tx.Exec(ctx, mustSQL("record_migration_checksum.sql"), file.Name, file.Checksum); err != nil {
		return fmt.Errorf("record checksum for %s: %w", file.Name, err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO public.schema_migrations(filename) VALUES($1)`, file.Name); err != nil {
		return fmt.Errorf("record application of %s: %w", file.Name, err)
	}

	if err := migrationsource.SettleConstraints(ctx, tx); err != nil {
		return fmt.Errorf("settle deferred effects of %s: %w", file.Name, err)
	}

	if err := file.Verify(ctx, tx); err != nil {
		return fmt.Errorf("verify migrated schema: %w", err)
	}

	return nil
}

type pgxAdvisoryLockSession struct {
	conn *pgxpool.Conn
}

var _ dbmigrate.UnlockFailureEvicter = pgxAdvisoryLockSession{}

func (s pgxAdvisoryLockSession) TryAdvisoryLock(ctx context.Context, key int64) (bool, error) {
	var acquired bool

	if err := s.conn.QueryRow(ctx, mustSQL("try_advisory_lock.sql"), key).Scan(&acquired); err != nil {
		return false, fmt.Errorf("scan try advisory lock: %w", err)
	}

	return acquired, nil
}

func (s pgxAdvisoryLockSession) AdvisoryUnlock(ctx context.Context, key int64) (bool, error) {
	var released bool

	if err := s.conn.QueryRow(ctx, mustSQL("advisory_unlock.sql"), key).Scan(&released); err != nil {
		return false, fmt.Errorf("scan advisory unlock: %w", err)
	}

	return released, nil
}

func (s pgxAdvisoryLockSession) EvictAfterUnlockFailure() error {
	if s.conn == nil {
		return errors.New("evict advisory lock conn: conn is nil")
	}

	conn := s.conn.Hijack()
	closeCtx, cancel := context.WithTimeout(context.Background(), advisoryUnlockTimeout)

	defer cancel()

	if err := conn.Close(closeCtx); err != nil {
		return fmt.Errorf("evict advisory lock conn: close: %w", err)
	}

	return nil
}
