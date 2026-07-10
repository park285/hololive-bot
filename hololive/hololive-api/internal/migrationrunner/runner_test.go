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

func TestBeginWrappedFileFailureRollsBackWholeTxBlock(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("BEGIN;\nCREATE TABLE tx_atomic_probe(id integer);\nSELECT 1/0;\nCOMMIT;\n")},
	}

	if _, err := Run(t.Context(), pool, fsys, Config{}); err == nil {
		t.Fatal("Run() error = nil, want failure from statement inside BEGIN block")
	}
	assertTableAbsent(t, pool, "tx_atomic_probe")
}

func TestAppliedMigrationChecksumMismatchFails(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	first := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 immutable.sql\n")},
		"immutable.sql":        {Data: []byte("CREATE TABLE immutable_v1(id integer)")},
	}
	if _, err := Run(t.Context(), pool, first, Config{}); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}

	modified := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 immutable.sql\n")},
		"immutable.sql":        {Data: []byte("CREATE TABLE immutable_v2(id integer)")},
	}
	_, err := Run(t.Context(), pool, modified, Config{})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("second Run() error = %v, want checksum mismatch", err)
	}
}

func TestFailedMigrationDoesNotPinBadChecksum(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	broken := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 repairable.sql\n")},
		"repairable.sql":       {Data: []byte("SELECT 1/0")},
	}
	if _, err := Run(t.Context(), pool, broken, Config{}); err == nil {
		t.Fatal("broken migration error = nil")
	}

	fixed := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 repairable.sql\n")},
		"repairable.sql":       {Data: []byte("CREATE TABLE repaired_after_failure(id integer)")},
	}
	if _, err := Run(t.Context(), pool, fixed, Config{}); err != nil {
		t.Fatalf("fixed migration error = %v", err)
	}
	assertTablePresent(t, pool, "repaired_after_failure")
}

func TestAppliedLedgerEntryBackfillsMissingChecksum(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `
		CREATE TABLE schema_migrations (
			filename text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		);
		INSERT INTO schema_migrations(filename) VALUES ('legacy.sql')
	`); err != nil {
		t.Fatalf("seed legacy ledger: %v", err)
	}

	content := []byte("SELECT 1")
	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 legacy.sql\n")},
		"legacy.sql":           {Data: content},
	}
	result, err := Run(ctx, pool, fsys, Config{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Fatalf("result = %+v, want one skipped migration", result)
	}

	var checksum string
	if err := pool.QueryRow(ctx, "SELECT checksum_sha256 FROM schema_migration_checksums WHERE filename = 'legacy.sql'").Scan(&checksum); err != nil {
		t.Fatalf("load backfilled checksum: %v", err)
	}
	if want := migrationChecksum(content); checksum != want {
		t.Fatalf("checksum = %q, want %q", checksum, want)
	}
}

func TestBeginWrappedFileAppliesTxBlockAndTrailingAutocommit(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("-- header comment\nBEGIN;\nCREATE TABLE tx_inside_ran(id integer);\nCOMMIT;\nCREATE TABLE tx_after_ran(id integer);\n")},
	}

	result, err := Run(t.Context(), pool, fsys, Config{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("result = %+v, want applied=1", result)
	}
	assertLedger(t, pool, []string{"tx.sql"})
	assertTablePresent(t, pool, "tx_inside_ran")
	assertTablePresent(t, pool, "tx_after_ran")
}

func TestBeginWrappedFileTrailingAutocommitFailureCanBeRerun(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("BEGIN;\nCREATE TABLE tx_committed_probe(id integer);\nCOMMIT;\nSELECT * FROM tx_missing_probe;\n")},
	}

	if _, err := Run(t.Context(), pool, fsys, Config{}); err == nil {
		t.Fatal("Run() error = nil, want trailing autocommit failure")
	}
	assertTablePresent(t, pool, "tx_committed_probe")
	assertTableAbsent(t, pool, "tx_tail_ran")
	assertLedger(t, pool, nil)

	fsys["tx.sql"] = &fstest.MapFile{Data: []byte("BEGIN;\nCREATE TABLE IF NOT EXISTS tx_committed_probe(id integer);\nCOMMIT;\nCREATE TABLE tx_tail_ran(id integer);\n")}
	result, err := Run(t.Context(), pool, fsys, Config{})
	if err != nil {
		t.Fatalf("Run() rerun error = %v", err)
	}
	if result.Applied != 1 || result.Skipped != 0 || result.Total != 1 {
		t.Fatalf("rerun result = %+v, want applied=1 skipped=0 total=1", result)
	}
	assertTablePresent(t, pool, "tx_committed_probe")
	assertTablePresent(t, pool, "tx_tail_ran")
	assertLedger(t, pool, []string{"tx.sql"})
}

func TestBeginWrappedFileWithConcurrentlyIsRejected(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("BEGIN;\nCREATE TABLE tx_conc_probe(id integer);\nCREATE INDEX CONCURRENTLY tx_conc_idx ON tx_conc_probe(id);\nCOMMIT;\n")},
	}

	_, err := Run(t.Context(), pool, fsys, Config{})
	if err == nil {
		t.Fatal("Run() error = nil, want explicit CONCURRENTLY-inside-BEGIN rejection")
	}
	if !strings.Contains(err.Error(), "CONCURRENTLY") {
		t.Fatalf("Run() error = %v, want CONCURRENTLY rejection", err)
	}
	assertTableAbsent(t, pool, "tx_conc_probe")
}

func TestBeginWrappedFileMissingCommitIsRejected(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 tx.sql\n")},
		"tx.sql":               {Data: []byte("BEGIN;\nCREATE TABLE tx_unclosed_probe(id integer);\n")},
	}

	_, err := Run(t.Context(), pool, fsys, Config{})
	if err == nil {
		t.Fatal("Run() error = nil, want missing-COMMIT rejection")
	}
	assertTableAbsent(t, pool, "tx_unclosed_probe")
}

func TestRunAppliesSessionTimeoutsToAllSegments(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 probe.sql\n")},
		"probe.sql": {Data: []byte(`BEGIN;
CREATE TABLE session_probe_tx AS
SELECT setting::bigint AS lock_timeout_ms FROM pg_settings WHERE name = 'lock_timeout';
COMMIT;
CREATE TABLE session_probe AS
SELECT
  (SELECT setting::bigint FROM pg_settings WHERE name = 'lock_timeout') AS lock_timeout_ms,
  (SELECT setting::bigint FROM pg_settings WHERE name = 'statement_timeout') AS statement_timeout_ms;
`)},
	}

	if _, err := Run(t.Context(), pool, fsys, Config{}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var lockMs, stmtMs int64
	if err := pool.QueryRow(t.Context(), "SELECT lock_timeout_ms, statement_timeout_ms FROM session_probe").Scan(&lockMs, &stmtMs); err != nil {
		t.Fatalf("read session probe: %v", err)
	}
	if lockMs != 10_000 {
		t.Errorf("autocommit segment lock_timeout = %dms, want 10000ms", lockMs)
	}
	if stmtMs != 240_000 {
		t.Errorf("autocommit segment statement_timeout = %dms, want 240000ms", stmtMs)
	}

	var txLockMs int64
	if err := pool.QueryRow(t.Context(), "SELECT lock_timeout_ms FROM session_probe_tx").Scan(&txLockMs); err != nil {
		t.Fatalf("read tx session probe: %v", err)
	}
	if txLockMs != 10_000 {
		t.Errorf("tx segment lock_timeout = %dms, want 10000ms", txLockMs)
	}
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
