// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dbtest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApplyMigrationsRollsBackBeginWrappedFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	writeMigrationFixture(t, dir, manifestFileName, "001 001_tx.sql\n")
	writeMigrationFixture(t, dir, "001_tx.sql", "BEGIN;\nCREATE TABLE tx_atomic_probe(id integer);\nSELECT 1/0;\nCOMMIT;\n")
	t.Setenv(migrationsDirEnv, dir)

	pool := NewBlankPool(t)
	ctx := context.Background()

	if err := ApplyMigrations(ctx, pool); err == nil {
		t.Fatal("ApplyMigrations() error = nil, want failure from statement inside BEGIN block")
	}

	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('tx_atomic_probe') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatalf("check tx_atomic_probe: %v", err)
	}
	if exists {
		t.Fatal("tx_atomic_probe present: BEGIN-wrapped migration failure must roll back the whole block")
	}
}

func TestApplyMigrationsAppliesBeginWrappedFileWithTrailingAutocommit(t *testing.T) {
	dir := t.TempDir()
	writeMigrationFixture(t, dir, manifestFileName, "001 001_tx.sql\n")
	writeMigrationFixture(t, dir, "001_tx.sql", "BEGIN;\nCREATE TABLE tx_inside_ran(id integer);\nCOMMIT;\nCREATE TABLE tx_after_ran(id integer);\n")
	t.Setenv(migrationsDirEnv, dir)

	pool := NewBlankPool(t)
	ctx := context.Background()

	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations() error = %v", err)
	}
	for _, table := range []string{"tx_inside_ran", "tx_after_ran"} {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s not present after BEGIN-wrapped migration", table)
		}
	}
}

func TestApplyMigrationsCanRerunAfterTrailingAutocommitFailure(t *testing.T) {
	dir := t.TempDir()
	writeMigrationFixture(t, dir, manifestFileName, "001 001_tx.sql\n")
	writeMigrationFixture(t, dir, "001_tx.sql", "BEGIN;\nCREATE TABLE dbtest_committed_probe(id integer);\nCOMMIT;\nSELECT * FROM dbtest_missing_probe;\n")
	t.Setenv(migrationsDirEnv, dir)

	pool := NewBlankPool(t)
	ctx := context.Background()

	if err := ApplyMigrations(ctx, pool); err == nil {
		t.Fatal("ApplyMigrations() error = nil, want trailing autocommit failure")
	}
	assertMigrationTablePresent(t, ctx, pool, "dbtest_committed_probe")
	assertMigrationTableAbsent(t, ctx, pool, "dbtest_tail_ran")

	writeMigrationFixture(t, dir, "001_tx.sql", "BEGIN;\nCREATE TABLE IF NOT EXISTS dbtest_committed_probe(id integer);\nCOMMIT;\nCREATE TABLE dbtest_tail_ran(id integer);\n")
	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("ApplyMigrations() rerun error = %v", err)
	}
	assertMigrationTablePresent(t, ctx, pool, "dbtest_committed_probe")
	assertMigrationTablePresent(t, ctx, pool, "dbtest_tail_ran")
}

func assertMigrationTablePresent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()

	if !migrationTableExists(t, ctx, pool, name) {
		t.Fatalf("table %s missing", name)
	}
}

func assertMigrationTableAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) {
	t.Helper()

	if migrationTableExists(t, ctx, pool, name) {
		t.Fatalf("table %s present", name)
	}
}

func migrationTableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) bool {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return exists
}

func writeMigrationFixture(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}
