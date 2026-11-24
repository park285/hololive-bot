package migrationrunner

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const emptyProbeRole = "migrationrunner_empty_probe"

func TestEmptyDatabaseProbeAcceptsWriterAdmissionForUnprivilegedMigrator(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	bootstrap, err := os.ReadFile("../../cmd/db-migrate/queries/bootstrap_writer_admission.sql")
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), "DO $role$ BEGIN CREATE ROLE postgres_admin SUPERUSER NOLOGIN; "+
		"EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $role$")
	require.NoError(t, err)

	admin, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	for _, statement := range []string{"SET ROLE postgres_admin", string(bootstrap), "RESET ROLE"} {
		_, err = admin.Exec(t.Context(), statement)
		require.NoError(t, err)
	}

	admin.Release()

	_, err = pool.Exec(t.Context(), "DROP ROLE IF EXISTS "+emptyProbeRole)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), "CREATE ROLE "+emptyProbeRole+" LOGIN PASSWORD 'empty-probe-password'")
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()

		if _, dropErr := pool.Exec(ctx, "DROP ROLE IF EXISTS "+emptyProbeRole); dropErr != nil {
			t.Errorf("drop probe role: %v", dropErr)
		}
	})

	config := pool.Config().ConnConfig.Copy()

	config.User, config.Password = emptyProbeRole, "empty-probe-password"

	conn, err := pgx.ConnectConfig(t.Context(), config)
	require.NoError(t, err)

	t.Cleanup(func() {
		if closeErr := conn.Close(context.WithoutCancel(t.Context())); closeErr != nil {
			t.Errorf("close probe connection: %v", closeErr)
		}
	})

	var empty bool

	require.NoError(t, conn.QueryRow(t.Context(), mustSQL("empty_database.sql")).Scan(&empty))
	require.True(t, empty, "a blank database with only the writer admission gate must count as empty")
}
