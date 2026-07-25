package migrationrunner

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/pkg/dbmigrate"

	"github.com/kapu/hololive-dbtest"
)

const blockingIndexDropSetupSQL = `
CREATE TABLE blocking_index_drop_probe(id integer);
CREATE INDEX blocking_index_drop_probe_idx ON blocking_index_drop_probe(id);
`

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
	assertIndexPresence(t, pool, "blocking_index_drop_probe_idx", false)
}

func TestExistingDatabaseRejectsBlockingIndexDropBeforeExecution(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	setupBlockingIndexDropProbe(t, pool)
	fsys := blockingIndexDropManifest("DROP INDEX IF EXISTS blocking_index_drop_probe_idx;")

	_, err := Run(t.Context(), pool, fsys, Config{})
	if err == nil {
		t.Fatal("Run() error = nil, want blocking DROP INDEX refusal")
	}
	if !strings.Contains(err.Error(), "MIGRATION_ALLOW_BLOCKING_INDEX_DROP=true") {
		t.Fatalf("Run() error = %v, want maintenance override guidance", err)
	}
	assertIndexPresence(t, pool, "blocking_index_drop_probe_idx", true)
	assertMigrationRecorded(t, pool, "drop.sql", false)
}

func TestExistingDatabaseMaintenanceOverrideAllowsBlockingIndexDrop(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	setupBlockingIndexDropProbe(t, pool)
	fsys := blockingIndexDropManifest("DROP INDEX IF EXISTS blocking_index_drop_probe_idx;")

	if _, err := Run(t.Context(), pool, fsys, Config{AllowBlockingIndexDrop: true}); err != nil {
		t.Fatalf("Run() maintenance override error = %v", err)
	}
	assertIndexPresence(t, pool, "blocking_index_drop_probe_idx", false)
	assertMigrationRecorded(t, pool, "drop.sql", true)
}

func TestExistingDatabaseAllowsConcurrentIndexDrop(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	setupBlockingIndexDropProbe(t, pool)
	fsys := blockingIndexDropManifest("DROP INDEX CONCURRENTLY IF EXISTS blocking_index_drop_probe_idx;")

	if _, err := Run(t.Context(), pool, fsys, Config{}); err != nil {
		t.Fatalf("Run() concurrent drop error = %v", err)
	}
	assertIndexPresence(t, pool, "blocking_index_drop_probe_idx", false)
	assertMigrationRecorded(t, pool, "drop.sql", true)
}

func setupBlockingIndexDropProbe(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	fsys := fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 setup.sql\n")},
		"setup.sql":           {Data: []byte(blockingIndexDropSetupSQL)},
	}
	if _, err := Run(t.Context(), pool, fsys, Config{}); err != nil {
		t.Fatalf("setup Run() error = %v", err)
	}
}

func blockingIndexDropManifest(dropSQL string) fstest.MapFS {
	return fstest.MapFS{
		dbmigrate.ManifestName: {Data: []byte("001 setup.sql\n002 drop.sql\n")},
		"setup.sql":           {Data: []byte(blockingIndexDropSetupSQL)},
		"drop.sql":            {Data: []byte(dropSQL)},
	}
}

func assertIndexPresence(t *testing.T, pool *pgxpool.Pool, name string, want bool) {
	t.Helper()
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
