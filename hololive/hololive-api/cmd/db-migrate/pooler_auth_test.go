package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestPoolerAuthBootstrap(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	if _, err := pool.Exec(t.Context(), "CREATE EXTENSION pg_stat_statements WITH SCHEMA public"); err != nil {
		t.Fatal(err)
	}

	checkPoolerAuthCredentials(t, pool)
	checkPoolerAuthPermissions(t, pool)
	checkPoolerLookupRejections(t, pool)
	checkPoolerBootstrapRejections(t, pool)
}

func checkPoolerAuthCredentials(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	t.Run("current SCRAM for all application roles", func(t *testing.T) {
		tx := poolerAuthTransaction(t, pool)
		execPoolerSQL(t, tx, mustSQL("bootstrap_pooler_auth.sql"))
		execPoolerSQL(t, tx, mustSQL("bootstrap_pooler_auth.sql"))

		for _, role := range loginRoles {
			first := lookupPoolerPassword(t, tx, role.user)
			assertCurrentPoolerPassword(t, tx, role.user, first)

			if err := upsertLoginRole(t.Context(), tx, role.user, role.user+"-fixture-password"); err != nil {
				t.Fatal(err)
			}

			unchanged := lookupPoolerPassword(t, tx, role.user)
			if first != unchanged {
				t.Fatal("unchanged password invalidated existing SCRAM frontend keys")
			}

			assertCurrentPoolerPassword(t, tx, role.user, unchanged)

			if err := upsertLoginRole(t.Context(), tx, role.user, role.user+"-rotated-fixture-password"); err != nil {
				t.Fatal(err)
			}

			rotated := lookupPoolerPassword(t, tx, role.user)
			if unchanged == rotated {
				t.Fatal("password rotation returned the previous verifier")
			}

			assertCurrentPoolerPassword(t, tx, role.user, rotated)
		}
	})
}

func checkPoolerAuthPermissions(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	t.Run("certificate-only restricted role", func(t *testing.T) {
		tx := poolerAuthTransaction(t, pool)
		execPoolerSQL(t, tx, mustSQL("bootstrap_pooler_auth.sql"))
		execPoolerSQL(t, tx, "CREATE TABLE public.pooler_private_data (id bigint)")

		var restricted bool

		err := tx.QueryRow(t.Context(), `SELECT rolcanlogin AND rolpassword IS NULL
			AND NOT (rolsuper OR rolcreatedb OR rolcreaterole OR rolinherit OR rolreplication OR rolbypassrls)
			AND rolconnlimit = 1 FROM pg_catalog.pg_authid WHERE rolname = 'hololive_pgbouncer_auth'`).Scan(&restricted)

		if err != nil || !restricted {
			t.Fatalf("certificate-only role attributes: restricted=%v, error=%v", restricted, err)
		}

		var publicSelectOnly bool

		err = tx.QueryRow(t.Context(), `SELECT
			pg_catalog.has_table_privilege('public', 'public.pg_stat_statements', 'SELECT')
			AND pg_catalog.has_table_privilege('public', 'public.pg_stat_statements_info', 'SELECT')
			AND NOT pg_catalog.has_schema_privilege('hololive_pgbouncer_auth', 'public', 'USAGE')`).Scan(&publicSelectOnly)
		if err != nil || !publicSelectOnly {
			t.Fatalf("extension PUBLIC SELECT behind restricted schema: got=%v, error=%v", publicSelectOnly, err)
		}

		for _, statement := range []string{
			"SELECT rolpassword FROM pg_catalog.pg_authid",
			"SELECT id FROM public.pooler_private_data",
			"SELECT 1 FROM public.pg_stat_statements LIMIT 0",
			"SELECT 1 FROM public.pg_stat_statements_info LIMIT 0",
			"CREATE TABLE public.pooler_forbidden (id bigint)",
			"CREATE TABLE hololive_pooler_auth.pooler_forbidden (id bigint)",
			"CREATE SCHEMA pooler_forbidden",
			"CREATE TEMP TABLE pooler_forbidden (id bigint)",
			"ALTER FUNCTION hololive_pooler_auth.lookup(text) OWNER TO hololive_pgbouncer_auth",
		} {
			assertPoolerPermissionDenied(t, tx, "hololive_pgbouncer_auth", statement)
		}
	})

	t.Run("application and public execution denied despite default grants", func(t *testing.T) {
		tx := poolerAuthTransaction(t, pool)
		execPoolerSQL(t, tx, "ALTER DEFAULT PRIVILEGES GRANT EXECUTE ON FUNCTIONS TO hololive_runtime")
		execPoolerSQL(t, tx, "ALTER DEFAULT PRIVILEGES GRANT USAGE ON SCHEMAS TO hololive_runtime")
		execPoolerSQL(t, tx, mustSQL("bootstrap_pooler_auth.sql"))
		execPoolerSQL(t, tx, "CREATE ROLE pooler_unrelated_client")

		roles := append([]loginRole{{user: "pooler_unrelated_client"}}, loginRoles...)
		for _, role := range roles {
			assertPoolerPermissionDenied(t, tx, role.user, "SELECT username, password FROM hololive_pooler_auth.lookup('hololive_runtime')")
			execPoolerSQL(t, tx, "SAVEPOINT schema_access")
			execPoolerSQL(t, tx, "GRANT USAGE ON SCHEMA hololive_pooler_auth TO "+pgx.Identifier{role.user}.Sanitize())
			assertPoolerPermissionDenied(t, tx, role.user, "SELECT username, password FROM hololive_pooler_auth.lookup('hololive_runtime')")
			execPoolerSQL(t, tx, "ROLLBACK TO SAVEPOINT schema_access")
			execPoolerSQL(t, tx, "RELEASE SAVEPOINT schema_access")
		}
	})
}

func checkPoolerLookupRejections(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	for _, test := range []struct {
		name      string
		statement string
	}{
		{"disabled", "ALTER ROLE hololive_runtime NOLOGIN"},
		{"expired", "ALTER ROLE hololive_runtime VALID UNTIL '2000-01-01'"},
		{"superuser", "ALTER ROLE hololive_runtime SUPERUSER"},
		{"replication", "ALTER ROLE hololive_runtime REPLICATION"},
		{"bypass RLS", "ALTER ROLE hololive_runtime BYPASSRLS"},
		{"create database", "ALTER ROLE hololive_runtime CREATEDB"},
		{"create role", "ALTER ROLE hololive_runtime CREATEROLE"},
		{"no password", "ALTER ROLE hololive_runtime PASSWORD NULL"},
		{"non SCRAM", "ALTER ROLE hololive_runtime PASSWORD 'md500000000000000000000000000000000'"},
	} {
		t.Run(test.name+" is not an authentication identity", func(t *testing.T) {
			tx := poolerAuthTransaction(t, pool)
			execPoolerSQL(t, tx, mustSQL("bootstrap_pooler_auth.sql"))
			execPoolerSQL(t, tx, test.statement)

			if lookupPoolerPassword(t, tx, "hololive_runtime") != "" {
				t.Fatal("ineligible role exposed an authentication verifier")
			}
		})
	}

	t.Run("unknown and privileged names return no rows", func(t *testing.T) {
		tx := poolerAuthTransaction(t, pool)
		execPoolerSQL(t, tx, mustSQL("bootstrap_pooler_auth.sql"))

		var admin string

		if err := tx.QueryRow(t.Context(), "SELECT CURRENT_USER").Scan(&admin); err != nil {
			t.Fatal(err)
		}

		for _, user := range []string{admin, "hololive_pgbouncer_auth", "hololive_migrator", "unknown", "hololive_runtime' OR true --"} {
			if lookupPoolerPassword(t, tx, user) != "" {
				t.Fatal("non-application identity exposed an authentication verifier")
			}
		}
	})
}

func checkPoolerBootstrapRejections(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	for _, test := range []struct {
		name      string
		statement string
	}{
		{"schema owner", "ALTER SCHEMA hololive_pooler_auth OWNER TO hololive_runtime"},
		{"function owner", "ALTER FUNCTION hololive_pooler_auth.lookup(text) OWNER TO hololive_runtime"},
		{"public schema usage", "GRANT USAGE ON SCHEMA hololive_pooler_auth TO PUBLIC"},
		{"application schema usage", "GRANT USAGE ON SCHEMA hololive_pooler_auth TO hololive_runtime"},
		{"auth schema create", "GRANT CREATE ON SCHEMA hololive_pooler_auth TO hololive_pgbouncer_auth"},
		{"schema grant option", "GRANT USAGE ON SCHEMA hololive_pooler_auth TO hololive_pgbouncer_auth WITH GRANT OPTION"},
		{"public function execute", "GRANT EXECUTE ON FUNCTION hololive_pooler_auth.lookup(text) TO PUBLIC"},
		{"application function execute", "GRANT EXECUTE ON FUNCTION hololive_pooler_auth.lookup(text) TO hololive_runtime"},
		{"function grant option", "GRANT EXECUTE ON FUNCTION hololive_pooler_auth.lookup(text) TO hololive_pgbouncer_auth WITH GRANT OPTION"},
		{"unrecognized function", "CREATE FUNCTION hololive_pooler_auth.unrecognized() RETURNS integer LANGUAGE sql SECURITY DEFINER AS 'SELECT 1'"},
		{"membership", "GRANT hololive_runtime TO hololive_pgbouncer_auth"},
		{"member", "GRANT hololive_pgbouncer_auth TO hololive_runtime"},
		{"owned object", "CREATE SCHEMA pooler_unexpected AUTHORIZATION hololive_pgbouncer_auth"},
		{"catalog grant", "GRANT SELECT ON pg_catalog.pg_authid TO hololive_pgbouncer_auth"},
		{"public catalog grant", "GRANT SELECT ON pg_catalog.pg_authid TO PUBLIC"},
		{"database create", "GRANT CREATE ON DATABASE %DATABASE% TO hololive_pgbouncer_auth"},
		{"public temp", "GRANT TEMPORARY ON DATABASE %DATABASE% TO PUBLIC"},
		{"connect grant option", "GRANT CONNECT ON DATABASE %DATABASE% TO hololive_pgbouncer_auth WITH GRANT OPTION"},
		{"public app data", "CREATE TABLE public.pooler_data (id bigint); GRANT SELECT ON public.pooler_data TO PUBLIC"},
		{"nonextension statistics view", "ALTER EXTENSION pg_stat_statements DROP VIEW public.pg_stat_statements"},
		{"reachable statistics views", "GRANT USAGE ON SCHEMA public TO PUBLIC"},
		{"auth statistics grant", "GRANT SELECT ON public.pg_stat_statements TO hololive_pgbouncer_auth"},
		{"public statistics update", "GRANT UPDATE ON public.pg_stat_statements TO PUBLIC"},
		{"public statistics column grant", "GRANT SELECT (userid) ON public.pg_stat_statements TO PUBLIC"},
	} {
		t.Run("reject unexpected "+test.name, func(t *testing.T) {
			tx := poolerAuthTransaction(t, pool)
			execPoolerSQL(t, tx, mustSQL("bootstrap_pooler_auth.sql"))

			password := lookupPoolerPassword(t, tx, "hololive_runtime")
			statement := strings.ReplaceAll(test.statement, "%DATABASE%", pgx.Identifier{pool.Config().ConnConfig.Database}.Sanitize())
			execPoolerSQL(t, tx, statement)

			const snapshotSQL = `SELECT concat_ws('|',
				(SELECT nspacl::text FROM pg_namespace WHERE nspname='hololive_pooler_auth'),
				(SELECT proacl::text FROM pg_proc WHERE oid='hololive_pooler_auth.lookup(text)'::regprocedure),
				(SELECT array_agg(oid ORDER BY oid)::text FROM pg_proc WHERE pronamespace='hololive_pooler_auth'::regnamespace))`

			var before, after string

			if err := tx.QueryRow(t.Context(), snapshotSQL).Scan(&before); err != nil {
				t.Fatal(err)
			}

			execPoolerSQL(t, tx, "SAVEPOINT bootstrap_rejection")

			_, err := tx.Exec(t.Context(), mustSQL("bootstrap_pooler_auth.sql"))
			pgErr, ok := errors.AsType[*pgconn.PgError](err)

			if !ok || pgErr.Code != "42501" {
				t.Fatalf("unsafe bootstrap error = %v, want SQLSTATE 42501", err)
			}

			execPoolerSQL(t, tx, "ROLLBACK TO SAVEPOINT bootstrap_rejection")
			assertCurrentPoolerPassword(t, tx, "hololive_runtime", password)

			if err := tx.QueryRow(t.Context(), snapshotSQL).Scan(&after); err != nil {
				t.Fatal(err)
			}

			if before != after {
				t.Fatal("rejected bootstrap changed authentication objects or privileges")
			}
		})
	}
}

func poolerAuthTransaction(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()

		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback pooler fixture: %v", err)
		}
	})
	execPoolerSQL(t, tx, "REVOKE ALL ON DATABASE "+pgx.Identifier{pool.Config().ConnConfig.Database}.Sanitize()+" FROM PUBLIC")
	execPoolerSQL(t, tx, "REVOKE ALL ON SCHEMA public FROM PUBLIC")

	for _, role := range loginRoles {
		if err := upsertLoginRole(t.Context(), tx, role.user, role.user+"-fixture-password"); err != nil {
			t.Fatal(err)
		}
	}

	return tx
}

func execPoolerSQL(t *testing.T, tx pgx.Tx, statement string) {
	t.Helper()

	if _, err := tx.Exec(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

func lookupPoolerPassword(t *testing.T, tx pgx.Tx, user string) string {
	t.Helper()
	execPoolerSQL(t, tx, "SET LOCAL ROLE hololive_pgbouncer_auth")

	var username, password string

	err := tx.QueryRow(t.Context(), "SELECT username, password FROM hololive_pooler_auth.lookup($1)", user).Scan(&username, &password)

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}

	execPoolerSQL(t, tx, "RESET ROLE")

	if err == nil && username != user {
		t.Fatal("lookup returned a different identity")
	}

	return password
}

func assertCurrentPoolerPassword(t *testing.T, tx pgx.Tx, user, password string) {
	t.Helper()

	var current bool

	err := tx.QueryRow(t.Context(), "SELECT rolpassword = $2 FROM pg_catalog.pg_authid WHERE rolname = $1", user, password).Scan(&current)

	if err != nil || !current || password == "" {
		t.Fatalf("lookup did not return the current verifier: %v", err)
	}
}

func assertPoolerPermissionDenied(t *testing.T, tx pgx.Tx, role, statement string) {
	t.Helper()
	execPoolerSQL(t, tx, "SAVEPOINT permission_probe")
	execPoolerSQL(t, tx, "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize())

	_, err := tx.Exec(t.Context(), statement)
	pgErr, ok := errors.AsType[*pgconn.PgError](err)

	if !ok || pgErr.Code != "42501" {
		t.Fatalf("role %s operation %q error = %v, want SQLSTATE 42501", role, statement, err)
	}

	execPoolerSQL(t, tx, "ROLLBACK TO SAVEPOINT permission_probe")
	execPoolerSQL(t, tx, "RELEASE SAVEPOINT permission_probe")
}
