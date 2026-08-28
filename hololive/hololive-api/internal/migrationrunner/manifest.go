package migrationrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"

	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

type migrationSource struct {
	name            string
	content         string
	checksum        string
	checksumPresent bool
}

const (
	youtubeJobLeaseFailureDiagnosticsMigration  = "177_youtube_job_lease_failure_diagnostics.sql"
	youtubeJobLeaseFailureDiagnosticsChecksum   = "37164dc07329a7d43d95058d77e7823e9dc42d8f66ae791b47d98b6522e49e9e"
	youtubeJobLeaseFailureDiagnosticsV1Checksum = "84023e0082c8ccccc880a40486330ad5d3ab2a520c3ee9ef412903767c152a6d"
	youtubeJobLeaseFailureDiagnosticsV2Checksum = "bad2f0359ff0bb3fbcac4bae0431780e456bf86faabdd2efdb4ed80b829dab9f"
)

func applyManifest(
	ctx context.Context,
	conn *pgxpool.Conn,
	fsys fs.FS,
	exec *guardedExecer,
	ledger dbmigrate.Ledger,
	entries []string,
	cfg Config,
) (Result, error) {
	querier := pgxRowQuerier{conn: conn}
	result := Result{Total: len(entries)}

	for _, name := range entries {
		source, err := loadMigrationSource(ctx, conn, fsys, name)
		if err != nil {
			return Result{}, fmt.Errorf("load migration source: %w", err)
		}

		applied, err := applyMigrationSource(ctx, exec, ledger, querier, source, cfg)
		if err != nil {
			return Result{}, fmt.Errorf("apply migration source: %w", err)
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
		return migrationSource{}, fmt.Errorf("load migration checksum: %w", err)
	}

	if present && !migrationChecksumMatches(name, stored, checksum) {
		return migrationSource{}, fmt.Errorf("migration %s checksum mismatch: ledger=%s source=%s", name, stored, checksum)
	}

	return migrationSource{name: name, content: string(content), checksum: checksum, checksumPresent: present}, nil
}

func migrationChecksumMatches(name, stored, source string) bool {
	if stored == source {
		return true
	}

	if name != youtubeJobLeaseFailureDiagnosticsMigration || source != youtubeJobLeaseFailureDiagnosticsChecksum {
		return false
	}

	// 두 hash는 migration 177이 불변이 되기 전에 배포되었습니다. 모든 지원 ledger를
	// 감사하고 migration 189가 상태를 정규화한 뒤에만 이 예외를 제거합니다.
	return stored == youtubeJobLeaseFailureDiagnosticsV1Checksum ||
		stored == youtubeJobLeaseFailureDiagnosticsV2Checksum
}

func applyMigrationSource(
	ctx context.Context,
	exec *guardedExecer,
	ledger dbmigrate.Ledger,
	querier dbmigrate.RowQuerier,
	source migrationSource,
	cfg Config,
) (bool, error) {
	alreadyApplied, err := ledger.Applied(ctx, querier, source.name)
	if err != nil {
		return false, fmt.Errorf("applied: %w", err)
	}

	if alreadyApplied {
		if err := skipAppliedMigration(source, cfg); err != nil {
			return false, fmt.Errorf("skip applied migration: %w", err)
		}

		return false, nil
	}

	cfg.logf("apply %s", source.name)

	if err := applyEntry(ctx, exec, ledger, source); err != nil {
		return false, fmt.Errorf("apply entry: %w", err)
	}

	return true, nil
}

func skipAppliedMigration(source migrationSource, cfg Config) error {
	if !source.checksumPresent {
		return fmt.Errorf(
			"migration %s is recorded in schema_migrations but its checksum is missing; refusing to trust the current source",
			source.name)
	}

	cfg.logf("skip %s (already applied)", source.name)

	return nil
}

func applyEntry(ctx context.Context, exec *guardedExecer, ledger dbmigrate.Ledger, source migrationSource) error {
	if err := exec.validateMigrationSource(source.name, source.content); err != nil {
		return fmt.Errorf("validate migration source: %w", err)
	}

	if source.name == epoch2Baseline {
		if err := applyEpoch2Baseline(ctx, exec, ledger, source); err != nil {
			return fmt.Errorf("apply epoch2 baseline: %w", err)
		}

		return nil
	}

	if err := exec.execFile(ctx, source.name, source.content); err != nil {
		return fmt.Errorf("exec file: %w", err)
	}

	if err := recordMigrationChecksum(ctx, exec.Exec, source.name, source.checksum); err != nil {
		return fmt.Errorf("record migration checksum: %w", err)
	}

	if err := ledger.Record(ctx, exec.Exec, source.name); err != nil {
		return fmt.Errorf("record: %w", err)
	}

	return nil
}

func applyEpoch2Baseline(ctx context.Context, exec *guardedExecer, ledger dbmigrate.Ledger, source migrationSource) error {
	segments, err := sqlsplit.Segments(source.content)
	if err != nil {
		return fmt.Errorf("exec %s: %w", source.name, err)
	}

	if len(segments) != 1 || !segments[0].Transactional {
		return fmt.Errorf("exec %s: epoch-2 baseline must be one top-level transaction", source.name)
	}

	tx, err := exec.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("exec %s: begin: %w", source.name, err)
	}

	if err := execTxStatements(ctx, tx, source.name, segments[0].Statements); err != nil {
		return fmt.Errorf("rollback tx segment on error: %w", rollbackTxSegmentOnError(ctx, tx, err))
	}

	txExec := txExecer(tx)
	if err := recordMigrationChecksum(ctx, txExec, source.name, source.checksum); err != nil {
		return fmt.Errorf("rollback tx segment on error: %w", rollbackTxSegmentOnError(ctx, tx, err))
	}

	if err := ledger.Record(ctx, txExec, source.name); err != nil {
		return fmt.Errorf("rollback tx segment on error: %w", rollbackTxSegmentOnError(ctx, tx, err))
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("exec %s: commit: %w", source.name, err)
	}

	return nil
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
