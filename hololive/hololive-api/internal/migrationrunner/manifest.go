package migrationrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"
)

type migrationSource struct {
	name            string
	content         string
	checksum        string
	checksumPresent bool
}

func applyManifest(ctx context.Context, conn *pgxpool.Conn, fsys fs.FS, exec *guardedExecer, ledger dbmigrate.Ledger, entries []string, cfg Config) (Result, error) {
	querier := pgxRowQuerier{conn: conn}
	result := Result{Total: len(entries)}
	for _, name := range entries {
		source, err := loadMigrationSource(ctx, conn, fsys, name)
		if err != nil {
			return Result{}, err
		}
		applied, err := applyMigrationSource(ctx, exec, ledger, querier, source, cfg)
		if err != nil {
			return Result{}, err
		}
		if applied {
			result.Applied++
		} else {
			result.Skipped++
		}
	}
	return result, nil
}

func loadMigrationSource(ctx context.Context, conn *pgxpool.Conn, fsys fs.FS, name string) (migrationSource, error) {
	content, err := fs.ReadFile(fsys, name)
	if err != nil {
		return migrationSource{}, fmt.Errorf("read migration %s: %w", name, err)
	}
	checksum := migrationChecksum(content)
	stored, present, err := loadMigrationChecksum(ctx, conn, name)
	if err != nil {
		return migrationSource{}, err
	}
	if present && stored != checksum {
		return migrationSource{}, fmt.Errorf("migration %s checksum mismatch: ledger=%s source=%s", name, stored, checksum)
	}
	return migrationSource{name: name, content: string(content), checksum: checksum, checksumPresent: present}, nil
}

func applyMigrationSource(ctx context.Context, exec *guardedExecer, ledger dbmigrate.Ledger, querier dbmigrate.RowQuerier, source migrationSource, cfg Config) (bool, error) {
	alreadyApplied, err := ledger.Applied(ctx, querier, source.name)
	if err != nil {
		return false, err
	}
	if alreadyApplied {
		return false, skipAppliedMigration(ctx, exec, source, cfg)
	}
	cfg.logf("apply %s", source.name)
	if err := applyEntry(ctx, exec, ledger, source); err != nil {
		return false, err
	}
	return true, nil
}

func skipAppliedMigration(ctx context.Context, exec *guardedExecer, source migrationSource, cfg Config) error {
	if !source.checksumPresent {
		if err := recordMigrationChecksum(ctx, exec.Exec, source.name, source.checksum); err != nil {
			return err
		}
	}
	cfg.logf("skip %s (already applied)", source.name)
	return nil
}

func applyEntry(ctx context.Context, exec *guardedExecer, ledger dbmigrate.Ledger, source migrationSource) error {
	if err := exec.execFile(ctx, source.name, source.content); err != nil {
		return err
	}
	if err := recordMigrationChecksum(ctx, exec.Exec, source.name, source.checksum); err != nil {
		return err
	}
	return ledger.Record(ctx, exec.Exec, source.name)
}

func migrationChecksum(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func ensureChecksumTable(ctx context.Context, exec dbmigrate.Execer) error {
	if err := exec(ctx, mustSQL("ensure_migration_checksums.sql")); err != nil {
		return fmt.Errorf("ensure migration checksum ledger: %w", err)
	}
	return nil
}

func loadMigrationChecksum(ctx context.Context, conn *pgxpool.Conn, name string) (checksum string, present bool, err error) {
	if scanErr := conn.QueryRow(ctx, mustSQL("checksum_by_filename.sql"), name).Scan(&checksum, &present); scanErr != nil {
		return "", false, fmt.Errorf("query migration checksum %s: %w", name, scanErr)
	}
	return checksum, present, nil
}

func recordMigrationChecksum(ctx context.Context, exec dbmigrate.Execer, name, checksum string) error {
	if err := exec(ctx, mustSQL("record_migration_checksum.sql"), name, checksum); err != nil {
		return fmt.Errorf("record migration checksum %s: %w", name, err)
	}
	return nil
}
