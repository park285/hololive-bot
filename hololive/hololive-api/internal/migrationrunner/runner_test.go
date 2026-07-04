package migrationrunner

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"

	"github.com/kapu/hololive-api/scripts/migrations"
	"github.com/kapu/hololive-shared/pkg/dbtest"
)

func TestFreshDBAppliesAllAndIgnoresBaselineWatermark(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		"first.sql":            {Data: []byte("CREATE TABLE baseline_first_ran(id integer)")},
		"second.sql":           {Data: []byte("CREATE TABLE baseline_second_ran(id integer)")},
	}

	result := runMigrations(t, pool, fsys, "second.sql")
	if result.Applied != 2 || result.Skipped != 0 || result.Total != 2 {
		t.Fatalf("result = %+v, want applied=2 skipped=0 total=2", result)
	}
	assertLedger(t, pool, []string{"first.sql", "second.sql"})
	assertTablePresent(t, pool, "baseline_first_ran")
	assertTablePresent(t, pool, "baseline_second_ran")
}

func TestPopulatedDBEmptyLedgerNoBaselineRefuses(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	seedBaseSchema(t, pool)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n")},
		"first.sql":            {Data: []byte("CREATE TABLE baseline_first_ran(id integer)")},
		"second.sql":           {Data: []byte("CREATE TABLE baseline_second_ran(id integer)")},
	}

	_, err := Run(t.Context(), pool, fsys, Config{})
	if err == nil {
		t.Fatal("Run() error = nil, want refusal on populated DB with empty ledger and no baseline")
	}
	if !strings.Contains(err.Error(), "empty schema_migrations ledger") {
		t.Fatalf("Run() error = %v, want empty-ledger refusal", err)
	}
	assertTableAbsent(t, pool, "baseline_first_ran")
	assertTableAbsent(t, pool, "baseline_second_ran")
}

func TestPopulatedDBEmptyLedgerBaselineStampsThenApplies(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	seedBaseSchema(t, pool)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 first.sql\n002 second.sql\n003 tail.sql\n")},
		"first.sql":            {Data: []byte("CREATE TABLE baseline_first_ran(id integer)")},
		"second.sql":           {Data: []byte("CREATE TABLE baseline_second_ran(id integer)")},
		"tail.sql":             {Data: []byte("CREATE TABLE baseline_tail_ran(id integer)")},
	}

	result := runMigrations(t, pool, fsys, "second.sql")
	if result.Applied != 1 || result.Skipped != 2 || result.Total != 3 {
		t.Fatalf("result = %+v, want applied=1 skipped=2 total=3", result)
	}
	assertLedger(t, pool, []string{"first.sql", "second.sql", "tail.sql"})
	assertTableAbsent(t, pool, "baseline_first_ran")
	assertTableAbsent(t, pool, "baseline_second_ran")
	assertTablePresent(t, pool, "baseline_tail_ran")
}

func TestRealManifestFullReplayOnBlankDB(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	entries := manifestEntries(t)
	result, err := Run(t.Context(), pool, migrations.FS, Config{Logf: t.Logf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	t.Logf("full replay result: %d applied / %d skipped (total %d)", result.Applied, result.Skipped, result.Total)

	if result.Applied != len(entries) || result.Skipped != 0 || result.Total != len(entries) {
		t.Fatalf("result = %+v, want applied=%d skipped=0 total=%d", result, len(entries), len(entries))
	}
	assertTablePresent(t, pool, "members")
	assertTablePresent(t, pool, "alarms")
}

func TestRealManifestPrefilledLedgerSkipsAll(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	entries := manifestEntries(t)
	prefillLedger(t, pool, entries)

	result, err := Run(t.Context(), pool, migrations.FS, Config{Logf: t.Logf})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	t.Logf("prefilled ledger result: %d applied / %d skipped (total %d)", result.Applied, result.Skipped, result.Total)

	if result.Applied != 0 || result.Skipped != len(entries) || result.Total != len(entries) {
		t.Fatalf("result = %+v, want applied=0 skipped=%d total=%d", result, len(entries), len(entries))
	}
}

func manifestEntries(t *testing.T) []string {
	t.Helper()

	entries, err := dbmigrate.Manifest(migrations.FS)
	if err != nil {
		t.Fatalf("read embedded manifest: %v", err)
	}
	return entries
}

func prefillLedger(t *testing.T, pool *pgxpool.Pool, entries []string) {
	t.Helper()

	ctx := t.Context()
	if _, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (filename TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())"); err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	for _, name := range entries {
		if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations(filename) VALUES ($1) ON CONFLICT (filename) DO NOTHING", name); err != nil {
			t.Fatalf("prefill ledger %s: %v", name, err)
		}
	}
}

func seedBaseSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	for _, ddl := range []string{
		"CREATE TABLE members(id integer)",
		"CREATE TABLE alarms(id integer)",
	} {
		if _, err := pool.Exec(t.Context(), ddl); err != nil {
			t.Fatalf("seed base schema: %v", err)
		}
	}
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
