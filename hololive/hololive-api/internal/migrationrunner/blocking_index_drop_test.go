package migrationrunner

import (
	"io/fs"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"

	"github.com/kapu/hololive-api/scripts/migrations"
	"github.com/kapu/hololive-dbtest"
)

const blockingIndexDropSetupSQL = `
CREATE TABLE blocking_index_drop_probe(id integer);
CREATE INDEX blocking_index_drop_probe_idx ON blocking_index_drop_probe(id);
`

func TestBlockingIndexDropSourcePolicy(t *testing.T) {
	for _, test := range []struct {
		name     string
		source   string
		allow    bool
		wantFail bool
	}{
		{
			name:     "plain multiline drop",
			source:   "DROP /* blocking */\nINDEX IF EXISTS unsafe_index;",
			wantFail: true,
		},
		{
			name:     "leading comment drop",
			source:   "-- maintenance DDL\nDROP INDEX IF EXISTS unsafe_index;",
			wantFail: true,
		},
		{
			name:   "concurrent drop",
			source: "DROP /* safe */ INDEX\nCONCURRENTLY IF EXISTS safe_index;",
		},
		{
			name:   "quoted body",
			source: "DO $body$ BEGIN RAISE NOTICE 'DROP INDEX is quoted'; END $body$;",
		},
		{
			name:   "maintenance override",
			source: "DROP INDEX IF EXISTS maintenance_index;",
			allow:  true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			exec := &guardedExecer{allowBlockingIndexDrop: test.allow}
			err := exec.validateMigrationSource("policy.sql", test.source)
			if test.wantFail && err == nil {
				t.Fatal("validateMigrationSource() error = nil, want blocking DROP INDEX refusal")
			}
			if !test.wantFail && err != nil {
				t.Fatalf("validateMigrationSource() error = %v", err)
			}
		})
	}
}

func TestCommittedMigrationsAfter114AvoidBlockingIndexDrops(t *testing.T) {
	entries, err := dbmigrate.Manifest(migrations.FS)
	if err != nil {
		t.Fatalf("read migration manifest: %v", err)
	}

	exec := &guardedExecer{}
	for _, name := range entries {
		number, numberErr := migrationNumber(name)
		if numberErr != nil {
			t.Fatalf("parse migration number %q: %v", name, numberErr)
		}
		if number <= 114 {
			continue
		}
		content, readErr := fs.ReadFile(migrations.FS, name)
		if readErr != nil {
			t.Fatalf("read migration %s: %v", name, readErr)
		}
		if validateErr := exec.validateMigrationSource(name, string(content)); validateErr != nil {
			t.Fatalf("migration %s violates blocking index-drop policy: %v", name, validateErr)
		}
	}
}

func TestFreshDatabaseAllowsBlockingIndexDropDuringBootstrap(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 bootstrap.sql\n")},
		"bootstrap.sql": {Data: []byte(blockingIndexDropSetupSQL + `
DROP INDEX IF EXISTS blocking_index_drop_probe_idx;
`)},
	}

	if _, err := Run(t.Context(), pool, fsys, Config{}); err != nil {
		t.Fatalf("Run() fresh bootstrap error = %v", err)
	}
	assertBlockingIndexPresence(t, pool, false)
}

func TestExistingDatabaseRejectsBlockingIndexDropBeforeExecution(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	setupBlockingIndexDropProbe(t, pool)
	fsys := blockingIndexDropManifest(`
CREATE TABLE blocking_index_drop_side_effect(id integer);
DROP /* blocking */
INDEX IF EXISTS blocking_index_drop_probe_idx;
`)

	_, err := Run(t.Context(), pool, fsys, Config{})
	if err == nil {
		t.Fatal("Run() error = nil, want blocking DROP INDEX refusal")
	}
	if !strings.Contains(err.Error(), "MIGRATION_ALLOW_BLOCKING_INDEX_DROP=true") {
		t.Fatalf("Run() error = %v, want maintenance override guidance", err)
	}
	assertTablePresence(t, pool, "blocking_index_drop_side_effect", false)
	assertBlockingIndexPresence(t, pool, true)
	assertMigrationRecorded(t, pool, "drop.sql", false)
}

func TestExistingDatabaseRejectsBlockingIndexDropBeforeAnyPendingMigrationExecution(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	setupBlockingIndexDropProbe(t, pool)
	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 setup.sql\n002 safe.sql\n003 drop.sql\n")},
		"setup.sql":            {Data: []byte(blockingIndexDropSetupSQL)},
		"safe.sql":             {Data: []byte("CREATE TABLE blocking_index_drop_early_side_effect(id integer);")},
		"drop.sql":             {Data: []byte("DROP INDEX IF EXISTS blocking_index_drop_probe_idx;")},
	}

	if _, err := Run(t.Context(), pool, fsys, Config{}); err == nil {
		t.Fatal("Run() error = nil, want blocking DROP INDEX refusal")
	}
	assertTablePresence(t, pool, "blocking_index_drop_early_side_effect", false)
	assertBlockingIndexPresence(t, pool, true)
	assertMigrationRecorded(t, pool, "safe.sql", false)
	assertMigrationRecorded(t, pool, "drop.sql", false)
}

func TestExistingDatabaseMaintenanceOverrideAllowsBlockingIndexDrop(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	setupBlockingIndexDropProbe(t, pool)
	fsys := blockingIndexDropManifest("DROP INDEX IF EXISTS blocking_index_drop_probe_idx;")

	if _, err := Run(t.Context(), pool, fsys, Config{AllowBlockingIndexDrop: true}); err != nil {
		t.Fatalf("Run() maintenance override error = %v", err)
	}
	assertBlockingIndexPresence(t, pool, false)
	assertMigrationRecorded(t, pool, "drop.sql", true)
}

func TestExistingDatabaseAllowsConcurrentIndexDrop(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	setupBlockingIndexDropProbe(t, pool)
	fsys := blockingIndexDropManifest("DROP INDEX CONCURRENTLY IF EXISTS blocking_index_drop_probe_idx;")

	if _, err := Run(t.Context(), pool, fsys, Config{}); err != nil {
		t.Fatalf("Run() concurrent drop error = %v", err)
	}
	assertBlockingIndexPresence(t, pool, false)
	assertMigrationRecorded(t, pool, "drop.sql", true)
}

func migrationNumber(name string) (int, error) {
	end := 0
	for end < len(name) && name[end] >= '0' && name[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, strconv.ErrSyntax
	}
	return strconv.Atoi(name[:end])
}

func setupBlockingIndexDropProbe(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 setup.sql\n")},
		"setup.sql":            {Data: []byte(blockingIndexDropSetupSQL)},
	}
	if _, err := Run(t.Context(), pool, fsys, Config{}); err != nil {
		t.Fatalf("setup Run() error = %v", err)
	}
}

func blockingIndexDropManifest(dropSQL string) fstest.MapFS {
	return fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 setup.sql\n002 drop.sql\n")},
		"setup.sql":            {Data: []byte(blockingIndexDropSetupSQL)},
		"drop.sql":             {Data: []byte(dropSQL)},
	}
}

func assertTablePresence(t *testing.T, pool *pgxpool.Pool, name string, want bool) {
	t.Helper()
	var present bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&present); err != nil {
		t.Fatalf("query table %s: %v", name, err)
	}
	if present != want {
		t.Fatalf("table %s present = %t, want %t", name, present, want)
	}
}

func assertBlockingIndexPresence(t *testing.T, pool *pgxpool.Pool, want bool) {
	t.Helper()
	const name = "blocking_index_drop_probe_idx"

	var present bool
	if err := pool.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&present); err != nil {
		t.Fatalf("query index %s: %v", name, err)
	}
	if present != want {
		t.Fatalf("index %s present = %t, want %t", name, present, want)
	}
}

func assertMigrationRecorded(t *testing.T, pool *pgxpool.Pool, name string, want bool) {
	t.Helper()
	var recorded bool
	if err := pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = $1)", name).Scan(&recorded); err != nil {
		t.Fatalf("query migration ledger %s: %v", name, err)
	}
	if recorded != want {
		t.Fatalf("migration %s recorded = %t, want %t", name, recorded, want)
	}
}
