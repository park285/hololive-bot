package migrationrunner

import (
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const epoch2Baseline = "001_schema_epoch2_baseline.sql"

//go:embed epoch2_legacy_contract.sha256
var epoch2LegacyContractRaw string

type epoch2LegacyMigration struct {
	name     string
	checksum string
}

func parseEpoch2LegacyContract(raw string) ([]epoch2LegacyMigration, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	contract := make([]epoch2LegacyMigration, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for index, line := range lines {
		checksum, name, found := strings.Cut(line, "  ")
		if !found || strings.ContainsAny(name, " \t\r\n") || filepath.Base(name) != name || !strings.HasSuffix(name, ".sql") {
			return nil, fmt.Errorf("line %d is malformed", index+1)
		}
		digest, err := hex.DecodeString(checksum)
		if err != nil || len(digest) != 32 || checksum != strings.ToLower(checksum) {
			return nil, fmt.Errorf("line %d has an invalid SHA-256", index+1)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("line %d duplicates %s", index+1, name)
		}
		seen[name] = struct{}{}
		contract = append(contract, epoch2LegacyMigration{name: name, checksum: checksum})
	}
	if len(contract) != 136 || contract[0].name != "006-base-runtime-tables.sql" || contract[len(contract)-1].name != "140_epoch2_checkpoint.sql" {
		return nil, fmt.Errorf("contract must span 006-base-runtime-tables.sql through 140_epoch2_checkpoint.sql")
	}
	return contract, nil
}

func verifyEpoch2LegacyContract(ctx context.Context, conn *pgxpool.Conn) error {
	contract, err := parseEpoch2LegacyContract(epoch2LegacyContractRaw)
	if err != nil {
		return fmt.Errorf("parse embedded contract: %w", err)
	}
	names := make([]string, len(contract))
	checksums := make([]string, len(contract))
	for index, migration := range contract {
		names[index] = migration.name
		checksums[index] = migration.checksum
	}

	rows, err := conn.Query(ctx, mustSQL("epoch2_legacy_contract.sql"), names, checksums)
	if err != nil {
		return fmt.Errorf("query legacy ledger: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, expected, actual string
		var ledgerPresent, checksumPresent bool
		if err := rows.Scan(&name, &expected, &ledgerPresent, &checksumPresent, &actual); err != nil {
			return fmt.Errorf("scan legacy ledger: %w", err)
		}
		switch {
		case !ledgerPresent:
			return fmt.Errorf("migration %s is missing from schema_migrations", name)
		case !checksumPresent:
			return fmt.Errorf("migration %s checksum is missing", name)
		case actual != expected:
			return fmt.Errorf("migration %s checksum mismatch: ledger=%s contract=%s", name, actual, expected)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read legacy ledger: %w", err)
	}
	return nil
}
