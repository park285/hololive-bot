package dbtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/migrationsource"
)

const (
	repoRootMarker          = "build-all.sh"
	migrationsRelDir        = "hololive/hololive-api/scripts/migrations"
	migrationsDirEnv        = "HOLOLIVE_MIGRATIONS_DIR"
	epoch4BaselineMigration = migrationsource.Baseline
	dbtestLedgerSchema      = "hololive_dbtest_internal"
)

func readMigrations() ([]migrationsource.File, error) {
	dir, err := resolveMigrationsDir()
	if err != nil {
		return nil, fmt.Errorf("resolve migrations: %w", err)
	}

	files, err := migrationsource.Load(os.DirFS(dir))
	if err != nil {
		return nil, fmt.Errorf("read dbtest migrations: %w", err)
	}

	return files, nil
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	files, err := readMigrations()
	if err != nil {
		return err
	}

	return installTestMigrations(ctx, pool, files)
}

func installTestMigrations(ctx context.Context, pool *pgxpool.Pool, files []migrationsource.File) error {
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var installed bool

		if err := tx.QueryRow(ctx, `SELECT to_regnamespace('hololive_dbtest_internal') IS NOT NULL`).Scan(&installed); err != nil {
			return fmt.Errorf("inspect dbtest schema: %w", err)
		}

		if installed {
			return advanceTestMigrations(ctx, tx, files)
		}

		var occupied bool

		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT FROM pg_depend WHERE refclassid='pg_namespace'::regclass
			AND refobjid='public'::regnamespace AND classid <> 'pg_default_acl'::regclass) OR EXISTS (
			SELECT FROM pg_namespace WHERE nspname NOT IN ('public','information_schema') AND nspname !~ '^pg_')`).Scan(&occupied); err != nil {
			return fmt.Errorf("inspect dbtest baseline target: %w", err)
		}

		if occupied {
			return errors.New("dbtest baseline requires an empty database")
		}

		if _, err := tx.Exec(ctx, `CREATE SCHEMA `+dbtestLedgerSchema+`; CREATE TABLE `+dbtestLedgerSchema+`.schema_migrations (
			filename text PRIMARY KEY, checksum_sha256 text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return fmt.Errorf("create dbtest ledger: %w", err)
		}

		return applyPending(ctx, tx, files)
	})
	if err != nil {
		return fmt.Errorf("install dbtest baseline: %w", err)
	}

	return nil
}

func advanceTestMigrations(ctx context.Context, tx pgx.Tx, files []migrationsource.File) error {
	rows, err := tx.Query(ctx, `SELECT filename,checksum_sha256 FROM `+dbtestLedgerSchema+`.schema_migrations ORDER BY filename COLLATE "C"`)
	if err != nil {
		return fmt.Errorf("read dbtest receipts: %w", err)
	}

	var i int

	for rows.Next() {
		var name, checksum string

		if err := rows.Scan(&name, &checksum); err != nil {
			rows.Close()

			return fmt.Errorf("decode dbtest receipt: %w", err)
		}

		if i == len(files) || name != files[i].Name || checksum != files[i].Checksum {
			rows.Close()

			return fmt.Errorf("unexpected dbtest receipt %s", name)
		}

		i++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read dbtest receipts: %w", err)
	}

	if i == 0 {
		return errors.New("empty dbtest ledger")
	}

	if err := files[i-1].Verify(ctx, tx); err != nil {
		return fmt.Errorf("verify dbtest applied schema: %w", err)
	}

	return applyPending(ctx, tx, files[i:])
}

func applyPending(ctx context.Context, tx pgx.Tx, files []migrationsource.File) error {
	for _, file := range files {
		if err := file.Apply(ctx, tx); err != nil {
			return fmt.Errorf("apply dbtest migration: %w", err)
		}

		if _, err := tx.Exec(ctx, `INSERT INTO `+dbtestLedgerSchema+`.schema_migrations(filename,checksum_sha256) VALUES ($1,$2)`, file.Name, file.Checksum); err != nil {
			return fmt.Errorf("record dbtest migration %s: %w", file.Name, err)
		}

		if err := migrationsource.SettleConstraints(ctx, tx); err != nil {
			return fmt.Errorf("settle deferred effects of %s: %w", file.Name, err)
		}

		if err := file.Verify(ctx, tx); err != nil {
			return fmt.Errorf("verify dbtest migration: %w", err)
		}
	}

	return nil
}

func resolveMigrationsDir() (string, error) {
	if env := strings.TrimSpace(os.Getenv(migrationsDirEnv)); env != "" {
		return env, nil
	}

	root, err := findRepoRoot()
	if err != nil {
		return "", fmt.Errorf("find repo root: %w", err)
	}

	return filepath.Join(root, migrationsRelDir), nil
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}

	dir := cwd

	for {
		if _, statErr := os.Stat(filepath.Join(dir, repoRootMarker)); statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo root marker %q not found above %q (set %s)", repoRootMarker, cwd, migrationsDirEnv)
		}

		dir = parent
	}
}
