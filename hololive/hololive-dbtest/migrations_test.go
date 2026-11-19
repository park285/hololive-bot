package dbtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBaselineRejectsUncommittedOrSplitFiles(t *testing.T) {
	for name, sql := range map[string]string{
		"late failure":           "BEGIN; CREATE TABLE probe(id int); SELECT 1/0; COMMIT;",
		"trailing autocommit":    "BEGIN; CREATE TABLE probe(id int); COMMIT; SELECT 1;",
		"concurrent index":       "BEGIN; CREATE TABLE probe(id int); CREATE INDEX CONCURRENTLY probe_idx ON probe(id); COMMIT;",
		"transaction detachment": "BEGIN; CREATE TABLE probe(id int); PREPARE TRANSACTION 'fixture_migration'; COMMIT;",
		"missing commit":         "BEGIN; CREATE TABLE probe(id int);",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(migrationsDirEnv, dir)
			require.NoError(t, os.Mkdir(filepath.Join(dir, "contracts"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "contracts", epoch4BaselineMigration), []byte("SELECT to_regclass('public.probe') IS NOT NULL"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte("001 "+epoch4BaselineMigration+"\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, epoch4BaselineMigration), []byte(sql), 0o600))

			pool := NewBlankPool(t)
			require.Error(t, ApplyMigrations(t.Context(), pool))

			var clean bool

			require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_regclass('probe') IS NULL AND to_regnamespace('hololive_dbtest_internal') IS NULL`).Scan(&clean))
			require.True(t, clean)
		})
	}
}
