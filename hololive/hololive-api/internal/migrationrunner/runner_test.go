package migrationrunner

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"

	"github.com/kapu/hololive-shared/pkg/dbtest"
)

func TestBaselineStampThenApplySkipsStampedMigrations(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	initialFS := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		"first.sql":            {Data: []byte("CREATE TABLE baseline_first_ran(id integer)")},
		"second.sql":           {Data: []byte("CREATE TABLE baseline_second_ran(id integer)")},
	}

	result := runMigrations(t, pool, initialFS, "second.sql")
	if result.Applied != 0 {
		t.Fatalf("initial applied = %d, want 0", result.Applied)
	}
	assertLedger(t, pool, []string{"first.sql", "second.sql"})
	assertTableAbsent(t, pool, "baseline_first_ran")
	assertTableAbsent(t, pool, "baseline_second_ran")

	tailFS := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n003 tail.sql\n")},
		"first.sql":            {Data: []byte("CREATE TABLE baseline_first_ran(id integer)")},
		"second.sql":           {Data: []byte("CREATE TABLE baseline_second_ran(id integer)")},
		"tail.sql":             {Data: []byte("CREATE TABLE baseline_tail_ran(id integer)")},
	}

	result = runMigrations(t, pool, tailFS, "second.sql")
	if result.Applied != 1 {
		t.Fatalf("tail applied = %d, want 1", result.Applied)
	}
	assertLedger(t, pool, []string{"first.sql", "second.sql", "tail.sql"})
	assertTablePresent(t, pool, "baseline_tail_ran")
}

func runMigrations(t *testing.T, pool *pgxpool.Pool, fsys fs.FS, baselineThrough string) Result {
	t.Helper()

	result, err := Run(t.Context(), pool, fsys, Config{BaselineThrough: baselineThrough})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return result
}

func assertLedger(t *testing.T, pool *pgxpool.Pool, want []string) {
	t.Helper()

	rows, err := pool.Query(t.Context(), "SELECT filename FROM schema_migrations ORDER BY filename")
	if err != nil {
		t.Fatalf("query ledger: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			t.Fatalf("scan ledger: %v", scanErr)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("ledger = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ledger = %v, want %v", got, want)
		}
	}
}

func assertTablePresent(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()

	if !tableExists(t, pool, name) {
		t.Fatalf("table %s missing", name)
	}
}

func assertTableAbsent(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()

	if tableExists(t, pool, name) {
		t.Fatalf("table %s present", name)
	}
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()

	var exists bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", name, err)
	}
	return exists
}
