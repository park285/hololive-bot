package migrationsource

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/park285/shared-go/v2/pkg/dbmigrate"

	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

const Baseline = "001_schema_epoch4_baseline.sql"

var suffixName = regexp.MustCompile(`^\d{3}_epoch4_[a-z0-9_]+\.sql$`)

//go:embed queries/settle_constraints.sql queries/prepare_migration_session.sql
var sqlAssets embed.FS

var (
	settleConstraints          = pgxdb.MustStatement(sqlAssets, "queries/settle_constraints.sql")
	prepareMigrationSessionSQL = pgxdb.MustStatement(sqlAssets, "queries/prepare_migration_session.sql")
)

//go:embed queries/physical_contract.sql.tpl
var physicalContractTemplateSQL string

type File struct {
	Name     string
	Checksum string
	SQL      []byte
	Contract string
}

func Load(source fs.FS) ([]File, error) {
	names, err := dbmigrate.Manifest(source)
	if err != nil {
		return nil, fmt.Errorf("read migration manifest: %w", err)
	}

	if names[0] != Baseline {
		return nil, fmt.Errorf("manifest must start with %s", Baseline)
	}

	files := make([]File, len(names))
	for i, name := range names {
		if i > 0 && (!suffixName.MatchString(name) || !strings.HasPrefix(name, fmt.Sprintf("%03d_", i+1))) {
			return nil, fmt.Errorf("invalid epoch-4 migration %d: %s", i+1, name)
		}

		raw, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		contract, err := fs.ReadFile(source, "contracts/"+name)
		if err != nil {
			return nil, fmt.Errorf("read physical contract for %s: %w", name, err)
		}

		digest := sha256.Sum256(raw)

		files[i] = File{Name: name, Checksum: hex.EncodeToString(digest[:]), SQL: raw, Contract: string(contract)}
	}

	return files, nil
}

func Receipts(files []File) string {
	var result strings.Builder

	for i, file := range files {
		if i != 0 {
			result.WriteByte('\n')
		}

		result.WriteString(file.Checksum)
		result.WriteString("  ")
		result.WriteString(file.Name)
	}

	return result.String()
}

func (f File) Apply(ctx context.Context, tx pgx.Tx) error {
	segments, err := sqlsplit.Segments(string(f.SQL))
	if err != nil {
		return fmt.Errorf("split %s: %w", f.Name, err)
	}

	if len(segments) != 1 || !segments[0].Transactional || len(segments[0].Statements) == 0 {
		return fmt.Errorf("%s must contain a single nonempty BEGIN/COMMIT block", f.Name)
	}

	if err := prepareMigrationSession(ctx, tx); err != nil {
		return err
	}

	for _, stmt := range segments[0].Statements {
		if _, err := tx.Exec(ctx, stmt); err != nil { //nolint:forbidigo // migration 파일에 담긴 문장을 실행한다.
			return fmt.Errorf("apply %s: %w", f.Name, err)
		}
	}

	return nil
}

func prepareMigrationSession(ctx context.Context, tx pgx.Tx) error {
	if _, err := pgxdb.Exec(ctx, tx, prepareMigrationSessionSQL); err != nil {
		return fmt.Errorf("prepare migration namespace and function validation: %w", err)
	}

	return nil
}

func SettleConstraints(ctx context.Context, tx pgx.Tx) error {
	if err := prepareMigrationSession(ctx, tx); err != nil {
		return err
	}

	if _, err := pgxdb.Exec(ctx, tx, settleConstraints); err != nil {
		return fmt.Errorf("settle named constraints and restore initial timing: %w", err)
	}

	return nil
}

func (f File) Verify(ctx context.Context, tx pgx.Tx) error {
	if err := prepareMigrationSession(ctx, tx); err != nil {
		return err
	}

	var ready bool

	predicate := strings.TrimSuffix(strings.TrimSpace(f.Contract), ";")

	if err := tx.QueryRow(ctx, strings.ReplaceAll(physicalContractTemplateSQL, "{{predicate}}", predicate), pgx.QueryExecModeExec).Scan(&ready); err != nil { //nolint:forbidigo // migration 파일이 선언한 물리 계약 술어를 검사한다.
		return fmt.Errorf("inspect physical contract for %s: %w", f.Name, err)
	}

	if !ready {
		return fmt.Errorf("physical contract for %s is not satisfied", f.Name)
	}

	return nil
}
