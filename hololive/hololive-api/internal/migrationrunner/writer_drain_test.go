package migrationrunner

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	drainMigratorRole = "hololive_migrator"
	drainWriterRole   = "hololive_collector_d"
	drainPassword     = "writer-admission-fixture-password"
)

func TestPendingMigrationWindowRequiresHeldDrainAndKeepsWritersClosed(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	bootstrap, err := os.ReadFile("../../cmd/db-migrate/queries/bootstrap_writer_admission.sql")
	require.NoError(t, err)

	for _, statement := range []string{
		"DO $role$ BEGIN CREATE ROLE postgres_admin SUPERUSER NOLOGIN; EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $role$",
		"DO $role$ BEGIN CREATE ROLE " + drainMigratorRole + " LOGIN; EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $role$",
		"DO $role$ BEGIN CREATE ROLE " + drainWriterRole + " LOGIN; EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $role$",
		"ALTER ROLE " + drainMigratorRole + " LOGIN NOSUPERUSER PASSWORD '" + drainPassword + "'",
		"ALTER ROLE " + drainWriterRole + " LOGIN NOSUPERUSER PASSWORD '" + drainPassword + "'",
	} {
		_, err = pool.Exec(t.Context(), statement)
		require.NoError(t, err)
	}

	migrator := drainConnect(t, pool, drainMigratorRole)
	require.False(t, drainState(t, migrator), "no drain guard session")

	admin, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	for _, statement := range []string{"SET ROLE postgres_admin", string(bootstrap), "RESET ROLE"} {
		_, err = admin.Exec(t.Context(), statement)
		require.NoError(t, err)
	}

	admin.Release()

	writer := drainConnect(t, pool, drainWriterRole)

	guard := drainConnect(t, pool, pool.Config().ConnConfig.User)
	_, err = guard.Exec(t.Context(), "SELECT set_config('application_name', 'hololive-writers-drained', false)")
	require.NoError(t, err)
	require.False(t, drainState(t, migrator), "drained name without the held guard lock")

	guardTx, err := guard.Begin(t.Context())
	require.NoError(t, err)

	var locked bool

	require.NoError(t, guardTx.QueryRow(t.Context(), "SELECT pg_try_advisory_xact_lock(0x484f4c4f, 6432)").Scan(&locked))
	require.True(t, locked)
	require.False(t, drainState(t, migrator), "an existing writer session keeps the window closed")

	require.NoError(t, writer.Close(t.Context()))
	require.Eventually(t, func() bool { return drainState(t, migrator) }, 10*time.Second, 50*time.Millisecond)

	window, err := migrator.Begin(t.Context())
	require.NoError(t, err)

	var drained bool

	require.NoError(t, window.QueryRow(t.Context(), mustSQL("writer_drain_state.sql")).Scan(&drained))
	require.True(t, drained)
	require.NoError(t, guard.Close(t.Context()))
	requireWriterLogin(t, pool, "57P03")

	require.NoError(t, window.Rollback(t.Context()))
	requireWriterLogin(t, pool, "")
}

func drainState(t *testing.T, conn *pgx.Conn) bool {
	t.Helper()

	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, tx.Rollback(context.WithoutCancel(t.Context()))) }()

	var drained bool

	require.NoError(t, tx.QueryRow(t.Context(), mustSQL("writer_drain_state.sql")).Scan(&drained))

	return drained
}

func drainConnect(t *testing.T, pool *pgxpool.Pool, user string) *pgx.Conn {
	t.Helper()

	config := pool.Config().ConnConfig.Copy()
	if user != config.User {
		config.User, config.Password = user, drainPassword
	}

	conn, err := pgx.ConnectConfig(t.Context(), config)
	require.NoError(t, err)

	t.Cleanup(func() {
		if !conn.IsClosed() {
			require.NoError(t, conn.Close(context.WithoutCancel(t.Context())))
		}
	})

	return conn
}

func requireWriterLogin(t *testing.T, pool *pgxpool.Pool, rejected string) {
	t.Helper()

	config := pool.Config().ConnConfig.Copy()
	config.User, config.Password = drainWriterRole, drainPassword

	conn, err := pgx.ConnectConfig(t.Context(), config)
	if rejected == "" {
		require.NoError(t, err)
		require.NoError(t, conn.Close(t.Context()))

		return
	}

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	require.True(t, ok, "writer login error = %v", err)
	require.Equal(t, rejected, pgErr.Code)
}
