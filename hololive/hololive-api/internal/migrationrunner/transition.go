package migrationrunner

import (
	"context"
	_ "embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"
)

// 삭제된 SQL을 다시 실행하지 않고 372 head의 완전한 적용 기록만 인정합니다.
//
//go:embed epoch2_head.sha256
var epoch2HeadSeal string

func transitionEpoch2(ctx context.Context, conn *pgxpool.Conn, fsys fs.FS, ledger dbmigrate.Ledger, entries []string) error {
	if len(entries) == 0 || entries[0] != epoch3Baseline {
		return nil
	}

	residue, err := hasLegacyResidue(ctx, conn, entries)
	if err != nil {
		return fmt.Errorf("inspect epoch ledger: %w", err)
	}

	if !residue {
		return nil
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin epoch transition: %w", err)
	}

	if err := replaceEpoch2Ledger(ctx, tx, fsys, ledger); err != nil {
		return rollbackTxSegmentOnError(ctx, tx, fmt.Errorf("epoch-2 ledger rejected; upgrade to 372 with the previous release before rerunning: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit epoch transition: %w", err)
	}

	return nil
}

func replaceEpoch2Ledger(ctx context.Context, tx pgx.Tx, fsys fs.FS, ledger dbmigrate.Ledger) error {
	if _, err := tx.Exec(ctx, mustSQL("lock_epoch_ledgers.sql")); err != nil {
		return fmt.Errorf("lock epoch ledgers: %w", err)
	}

	if err := validateEpoch2Head(ctx, tx); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, mustSQL("clear_epoch_ledgers.sql")); err != nil {
		return fmt.Errorf("clear epoch ledgers: %w", err)
	}

	if err := recordBaselineEntry(ctx, fsys, ledger, txExecer(tx), epoch3Baseline); err != nil {
		return fmt.Errorf("record epoch-3 baseline: %w", err)
	}

	return nil
}

func validateEpoch2Head(ctx context.Context, tx pgx.Tx) error {
	sealed := make(map[string]string, 233)

	for line := range strings.SplitSeq(strings.TrimSpace(epoch2HeadSeal), "\n") {
		checksum, filename, found := strings.Cut(line, "  ")
		if !found || len(checksum) != 64 || filename == "" {
			return fmt.Errorf("invalid embedded epoch-2 seal: %q", line)
		}

		sealed[filename] = checksum
	}

	rows, err := tx.Query(ctx, mustSQL("epoch_ledger_receipts.sql"))
	if err != nil {
		return fmt.Errorf("read epoch receipts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			filename, checksum string
			applied            bool
		)

		if err := rows.Scan(&filename, &checksum, &applied); err != nil {
			return fmt.Errorf("scan epoch receipt: %w", err)
		}

		want, exists := sealed[filename]
		if !exists || !applied || checksum != want {
			return fmt.Errorf("unexpected, incomplete or checksum-mismatched receipt %s", filename)
		}

		delete(sealed, filename)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read epoch receipts: %w", err)
	}

	if len(sealed) != 0 {
		return fmt.Errorf("incomplete epoch-2 ledger: %d receipts missing", len(sealed))
	}

	return nil
}
