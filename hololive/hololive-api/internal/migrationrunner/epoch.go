package migrationrunner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"
)

const (
	epoch2Baseline            = "001_schema_epoch2_baseline.sql"
	epoch2LegacyLedgerCleanup = "182_epoch2_legacy_ledger_cleanup.sql"
)

// reconcileBaseline은 apply-all.sh의 ledger 결정 블록을 포팅한다. 핵심 제약: 기존
// 스키마 + 빈 ledger + watermark 미지정이면 전체 manifest를 applied로 stamp해 아직
// 미적용인 마이그레이션이 조용히 skip되는 사고(073 DB에 074-082 유실)가 나므로 거부한다.
func reconcileBaseline(ctx context.Context, conn *pgxpool.Conn, fsys fs.FS, ledger dbmigrate.Ledger, entries []string, cfg Config) error {
	count, err := ledgerCount(ctx, conn)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	baseSchema, err := baseSchemaPresent(ctx, conn)
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
	if err := recordBaselineThrough(ctx, conn, fsys, ledger, entries, through); err != nil {
		return fmt.Errorf("baseline migrations: %w", err)
	}
	return nil
}

func recordBaselineThrough(
	ctx context.Context,
	conn *pgxpool.Conn,
	fsys fs.FS,
	ledger dbmigrate.Ledger,
	entries []string,
	through string,
) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	txExec := func(ctx context.Context, query string, args ...any) error {
		_, err := tx.Exec(ctx, query, args...)
		return err
	}
	if err := recordBaselineEntries(ctx, fsys, ledger, txExec, entries, through); err != nil {
		return rollbackTxSegmentOnError(ctx, tx, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func recordBaselineEntries(
	ctx context.Context,
	fsys fs.FS,
	ledger dbmigrate.Ledger,
	exec dbmigrate.Execer,
	entries []string,
	through string,
) error {
	for _, name := range entries {
		if err := recordBaselineEntry(ctx, fsys, ledger, exec, name); err != nil {
			return err
		}
		if name == through {
			return nil
		}
	}
	return fmt.Errorf("baseline target %s was not reached", through)
}

func recordBaselineEntry(ctx context.Context, fsys fs.FS, ledger dbmigrate.Ledger, exec dbmigrate.Execer, name string) error {
	content, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if err := recordMigrationChecksum(ctx, exec, name, migrationChecksum(content)); err != nil {
		return err
	}
	return ledger.Record(ctx, exec, name)
}

// manifest 밖 ledger 항목(이전 epoch 잔재)이 있는 DB는 checkpoint를 거친 경우에만
// 진행한다. 잔재는 epoch2LegacyLedgerCleanup이 같은 실행 안에서 지우므로 그 전까지만
// 허용하고, 정리가 적용된 뒤에도 남은 잔재는 출처를 알 수 없는 행이라 거부한다 —
// reconcileBaseline은 빈 ledger만 다룬다.
func guardEpochResidue(ctx context.Context, conn *pgxpool.Conn, ledger dbmigrate.Ledger, entries []string) error {
	if len(entries) == 0 {
		return nil
	}
	baseline := entries[0]
	residue, err := hasLegacyResidue(ctx, conn, entries)
	if err != nil {
		return err
	}
	querier := pgxRowQuerier{conn: conn}
	applied, err := ledger.Applied(ctx, querier, baseline)
	if err != nil {
		return err
	}
	if residue {
		return validateLegacyResidue(ctx, ledger, querier, baseline, applied)
	}
	return validateCurrentEpochBaseline(ctx, conn, baseline, applied)
}

func hasLegacyResidue(ctx context.Context, conn *pgxpool.Conn, entries []string) (bool, error) {
	var residue bool
	if err := conn.QueryRow(ctx, mustSQL("legacy_residue_present.sql"), entries).Scan(&residue); err != nil {
		return false, fmt.Errorf("detect legacy ledger residue: %w", err)
	}
	return residue, nil
}

func validateLegacyResidue(ctx context.Context, ledger dbmigrate.Ledger, querier dbmigrate.RowQuerier, baseline string, applied bool) error {
	if !applied {
		return fmt.Errorf(
			"schema_migrations has entries outside the current manifest but epoch baseline %s is not recorded; "+
				"this database predates the epoch squash without the checkpoint migration — deploy the checkpoint release first",
			baseline)
	}
	if baseline != epoch2Baseline {
		return nil
	}
	cleanupApplied, err := ledger.Applied(ctx, querier, epoch2LegacyLedgerCleanup)
	if err != nil {
		return err
	}
	if cleanupApplied {
		return fmt.Errorf(
			"schema_migrations still has entries outside the current manifest after %s was applied; "+
				"they are not the epoch-2 legacy ledger, so remove them manually before rerunning",
			epoch2LegacyLedgerCleanup)
	}
	return nil
}

func validateCurrentEpochBaseline(ctx context.Context, conn *pgxpool.Conn, baseline string, applied bool) error {
	if baseline != epoch2Baseline || !applied {
		return nil
	}
	_, checksumPresent, err := loadMigrationChecksum(ctx, conn, baseline)
	if err != nil {
		return err
	}
	if checksumPresent {
		return nil
	}
	return fmt.Errorf(
		"epoch baseline %s is recorded without its checksum and no legacy ledger residue remains; "+
			"refusing to trust a marker that cannot be proven by a completed R2 application",
		baseline)
}

func containsEntry(entries []string, target string) bool {
	return slices.Contains(entries, target)
}

func ledgerCount(ctx context.Context, conn *pgxpool.Conn) (int64, error) {
	var count int64
	if err := conn.QueryRow(ctx, mustSQL("ledger_count.sql")).Scan(&count); err != nil {
		return 0, fmt.Errorf("count schema_migrations: %w", err)
	}
	return count, nil
}

func baseSchemaPresent(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var present bool
	query := mustSQL("base_schema_present.sql")
	if err := conn.QueryRow(ctx, query).Scan(&present); err != nil {
		return false, fmt.Errorf("detect base schema: %w", err)
	}
	return present, nil
}
