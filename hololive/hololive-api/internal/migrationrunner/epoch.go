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

const epoch2Baseline = "001_schema_epoch2_baseline.sql"

// reconcileBaseline은 삭제된 셸 러너 apply-all.sh의 ledger 결정 블록을 포팅한다. 핵심 제약: 기존
// 스키마 + 빈 ledger + watermark 미지정이면 전체 manifest를 applied로 stamp해 아직
// 미적용인 마이그레이션이 조용히 skip되는 사고(073 DB에 074-082 유실)가 나므로 거부한다.
func reconcileBaseline(ctx context.Context, conn *pgxpool.Conn, fsys fs.FS, ledger dbmigrate.Ledger, entries []string, cfg Config) error {
	count, err := ledgerCount(ctx, conn)
	if err != nil {
		return fmt.Errorf("ledger count: %w", err)
	}

	if count > 0 {
		return nil
	}

	baseSchema, err := baseSchemaPresent(ctx, conn)
	if err != nil {
		return fmt.Errorf("base schema present: %w", err)
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

	txExec := txExecer(tx)
	if err := recordBaselineEntries(ctx, fsys, ledger, txExec, entries, through); err != nil {
		if rollbackErr := rollbackTxSegmentOnError(ctx, tx, err); rollbackErr != nil {
			return fmt.Errorf("rollback tx segment on error: %w", rollbackErr)
		}

		return nil
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
			return fmt.Errorf("record baseline entry: %w", err)
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
		return fmt.Errorf("record migration checksum: %w", err)
	}

	if err := ledger.Record(ctx, exec, name); err != nil {
		return fmt.Errorf("record: %w", err)
	}

	return nil
}

// 현재 manifest 밖 ledger 항목은 출처를 증명할 수 없으므로 존재하면 즉시 거부한다. 빈 ledger는 reconcileBaseline이 다룬다.
// 예전에는 epoch-1 ledger 잔재를 182_epoch2_legacy_ledger_cleanup.sql이 같은 실행 안에서 지우도록 182 적용 전까지 허용하고
// epoch-1 checkpoint baseline도 받았다. T18(2026-09-26)에서 운영 schema_migrations에 001·182가 기록되고 epoch-1 파일명
// 행이 0건임을 확인했고, DEC-20260926-hololive-retired-rollback-tooling이 epoch-1 rollback 창을 닫아 그 허용 분기를
// 지웠다(stack-audit 2026-09-26 T17). 이 거부는 드레인 종단이 아니라 ledger 무결성 계약이라 제거 조건이 없다.
func guardEpochResidue(ctx context.Context, conn *pgxpool.Conn, ledger dbmigrate.Ledger, entries []string) error {
	if len(entries) == 0 {
		return nil
	}

	residue, err := hasLegacyResidue(ctx, conn, entries)
	if err != nil {
		return fmt.Errorf("has legacy residue: %w", err)
	}

	if residue {
		return errors.New(
			"schema_migrations has entries outside the current manifest; the epoch-1 ledger window is closed, " +
				"so confirm where those rows came from and remove them manually before rerunning")
	}

	baseline := entries[0]

	applied, err := ledger.Applied(ctx, pgxRowQuerier{conn: conn}, baseline)
	if err != nil {
		return fmt.Errorf("applied: %w", err)
	}

	if err := validateCurrentEpochBaseline(ctx, conn, baseline, applied); err != nil {
		return fmt.Errorf("validate current epoch baseline: %w", err)
	}

	return nil
}

func hasLegacyResidue(ctx context.Context, conn *pgxpool.Conn, entries []string) (bool, error) {
	var residue bool

	if err := conn.QueryRow(ctx, mustSQL("legacy_residue_present.sql"), entries).Scan(&residue); err != nil {
		return false, fmt.Errorf("detect legacy ledger residue: %w", err)
	}

	return residue, nil
}

func validateCurrentEpochBaseline(ctx context.Context, conn *pgxpool.Conn, baseline string, applied bool) error {
	if baseline != epoch2Baseline || !applied {
		return nil
	}

	_, checksumPresent, err := loadMigrationChecksum(ctx, conn, baseline)
	if err != nil {
		return fmt.Errorf("load migration checksum: %w", err)
	}

	if checksumPresent {
		return nil
	}

	return fmt.Errorf(
		"epoch baseline %s is recorded without its checksum; "+
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
