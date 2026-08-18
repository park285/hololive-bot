package migrationrunner

import (
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
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
		migration, err := parseEpoch2LegacyLine(line, index+1)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[migration.name]; exists {
			return nil, fmt.Errorf("line %d duplicates %s", index+1, migration.name)
		}
		seen[migration.name] = struct{}{}
		contract = append(contract, migration)
	}
	if err := validateEpoch2LegacyRange(contract); err != nil {
		return nil, err
	}
	return contract, nil
}

func parseEpoch2LegacyLine(line string, number int) (epoch2LegacyMigration, error) {
	checksum, name, found := strings.Cut(line, "  ")
	if !found || !validEpoch2LegacyName(name) {
		return epoch2LegacyMigration{}, fmt.Errorf("line %d is malformed", number)
	}
	if !validEpoch2LegacyChecksum(checksum) {
		return epoch2LegacyMigration{}, fmt.Errorf("line %d has an invalid SHA-256", number)
	}
	return epoch2LegacyMigration{name: name, checksum: checksum}, nil
}

func validEpoch2LegacyName(name string) bool {
	return !strings.ContainsAny(name, " \t\r\n") && filepath.Base(name) == name && strings.HasSuffix(name, ".sql")
}

func validEpoch2LegacyChecksum(checksum string) bool {
	digest, err := hex.DecodeString(checksum)
	return err == nil && len(digest) == 32 && checksum == strings.ToLower(checksum)
}

func validateEpoch2LegacyRange(contract []epoch2LegacyMigration) error {
	if len(contract) != 136 {
		return fmt.Errorf("contract must contain 136 migrations")
	}
	if contract[0].name != "006-base-runtime-tables.sql" || contract[len(contract)-1].name != "140_epoch2_checkpoint.sql" {
		return fmt.Errorf("contract must span 006-base-runtime-tables.sql through 140_epoch2_checkpoint.sql")
	}
	return nil
}

func verifyEpoch2LegacyContract(ctx context.Context, conn *pgxpool.Conn) error {
	contract, err := parseEpoch2LegacyContract(epoch2LegacyContractRaw)
	if err != nil {
		return fmt.Errorf("parse embedded contract: %w", err)
	}
	names, checksums := epoch2LegacyColumns(contract)
	rows, err := conn.Query(ctx, mustSQL("epoch2_legacy_contract.sql"), names, checksums)
	if err != nil {
		return fmt.Errorf("query legacy ledger: %w", err)
	}
	defer rows.Close()
	return verifyEpoch2LegacyRows(rows)
}

func epoch2LegacyColumns(contract []epoch2LegacyMigration) (names, checksums []string) {
	names = make([]string, len(contract))
	checksums = make([]string, len(contract))
	for index, migration := range contract {
		names[index] = migration.name
		checksums[index] = migration.checksum
	}
	return names, checksums
}

func verifyEpoch2LegacyRows(rows pgx.Rows) error {
	for rows.Next() {
		var name, expected, actual string
		var ledgerPresent, checksumPresent bool
		if err := rows.Scan(&name, &expected, &ledgerPresent, &checksumPresent, &actual); err != nil {
			return fmt.Errorf("scan legacy ledger: %w", err)
		}
		if err := validateEpoch2LegacyRow(name, expected, actual, ledgerPresent, checksumPresent); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read legacy ledger: %w", err)
	}
	return nil
}

func validateEpoch2LegacyRow(name, expected, actual string, ledgerPresent, checksumPresent bool) error {
	switch {
	case !ledgerPresent:
		return fmt.Errorf("migration %s is missing from schema_migrations", name)
	case !checksumPresent:
		return fmt.Errorf("migration %s checksum is missing", name)
	case actual != expected:
		return fmt.Errorf("migration %s checksum mismatch: ledger=%s contract=%s", name, actual, expected)
	default:
		return nil
	}
}
