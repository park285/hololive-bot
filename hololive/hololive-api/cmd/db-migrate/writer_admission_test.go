package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

const (
	admissionWriterPassword = "writer-admission-fixture-password"
	admissionClosed         = "57P03"
	admissionPrivacySQL     = "SELECT NOT has_function_privilege('public', 'hololive_writer_admission.reject_during_writer_drain()', 'EXECUTE') " +
		"AND NOT has_schema_privilege('public', 'hololive_writer_admission', 'USAGE')"
)

func TestWriterAdmissionClosesApplicationLoginsWhileWritersDrain(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	bootstrapWriterAdmission(t, pool)
	bootstrapWriterAdmission(t, pool)

	writer := runtimepolicy.PostgresCollectorDRoleUser
	if err := upsertLoginRole(t.Context(), pool, writer, admissionWriterPassword); err != nil {
		t.Fatal(err)
	}

	execAdmission(t, pool, "DROP ROLE IF EXISTS writer_admission_other")
	execAdmission(t, pool, "CREATE ROLE writer_admission_other LOGIN PASSWORD '"+admissionWriterPassword+"'")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()

		if _, err := pool.Exec(ctx, "DROP ROLE IF EXISTS writer_admission_other"); err != nil {
			t.Errorf("drop fixture role: %v", err)
		}
	})

	requireLogin(t, pool, writer, "")

	admin := adminSession(t, pool)
	requireLogin(t, pool, writer, "")

	setApplication(t, admin, "hololive-writers-draining")
	requireLogin(t, pool, writer, admissionClosed)
	requireLogin(t, pool, "writer_admission_other", "")
	requireLogin(t, pool, pool.Config().ConnConfig.User, "")

	setApplication(t, admin, "hololive-writers-drained")
	requireLogin(t, pool, writer, admissionClosed)

	adminPID := int64(admin.PgConn().PID())
	if err := admin.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(t.Context(), "SELECT pg_terminate_backend($1::integer, 10000)", adminPID); err != nil {
		t.Fatal(err)
	}

	requireLogin(t, pool, writer, "")

	var private bool

	if err := pool.QueryRow(t.Context(), admissionPrivacySQL).Scan(&private); err != nil || !private {
		t.Fatalf("writer admission objects are reachable by PUBLIC: private=%v err=%v", private, err)
	}
}

func TestWriterAdmissionBootstrapRejectsUnexpectedObjects(t *testing.T) {
	for _, test := range []struct {
		name      string
		statement string
	}{
		{"function owner", "ALTER FUNCTION hololive_writer_admission.reject_during_writer_drain() OWNER TO writer_admission_owner"},
		{"schema owner", "ALTER SCHEMA hololive_writer_admission OWNER TO writer_admission_owner"},
		{"schema contents", "CREATE FUNCTION hololive_writer_admission.unexpected() RETURNS integer LANGUAGE sql AS 'SELECT 1'"},
		{"trigger event", "DROP EVENT TRIGGER hololive_writer_admission; " +
			"CREATE EVENT TRIGGER hololive_writer_admission ON ddl_command_start EXECUTE FUNCTION hololive_writer_admission.reject_during_writer_drain()"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := dbtest.NewBlankPool(t)
			bootstrapWriterAdmission(t, pool)

			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() {
				if rollbackErr := tx.Rollback(context.WithoutCancel(t.Context())); rollbackErr != nil {
					t.Errorf("rollback fixture transaction: %v", rollbackErr)
				}
			})

			for _, statement := range []string{"CREATE ROLE writer_admission_owner", test.statement} {
				if _, execErr := tx.Exec(t.Context(), statement); execErr != nil {
					t.Fatal(execErr)
				}
			}

			_, err = tx.Exec(t.Context(), mustSQL("bootstrap_writer_admission.sql"))
			if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "42501" {
				t.Fatalf("unsafe bootstrap error = %v, want SQLSTATE 42501", err)
			}
		})
	}
}

func bootstrapWriterAdmission(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	execAdmission(t, pool, mustSQL("bootstrap_writer_admission.sql"))
}

func execAdmission(t *testing.T, pool *pgxpool.Pool, statement string) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

func adminSession(t *testing.T, pool *pgxpool.Pool) *pgx.Conn {
	t.Helper()

	conn, err := pgx.ConnectConfig(t.Context(), pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := conn.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("close admin session: %v", err)
		}
	})

	return conn
}

func setApplication(t *testing.T, conn *pgx.Conn, name string) {
	t.Helper()

	if _, err := conn.Exec(t.Context(), "SELECT set_config('application_name', $1, false)", name); err != nil {
		t.Fatal(err)
	}
}

func tryLogin(t *testing.T, pool *pgxpool.Pool, user string) error {
	t.Helper()

	config := pool.Config().ConnConfig.Copy()
	if user != config.User {
		config.User, config.Password = user, admissionWriterPassword
	}

	conn, err := pgx.ConnectConfig(t.Context(), config)
	if err != nil {
		return err
	}

	return conn.Close(t.Context())
}

func requireLogin(t *testing.T, pool *pgxpool.Pool, user, rejected string) {
	t.Helper()

	err := tryLogin(t, pool, user)

	if rejected == "" {
		if err != nil {
			t.Fatalf("%s login failed: %v", user, err)
		}

		return
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != rejected {
		t.Fatalf("%s login error = %v, want SQLSTATE %s", user, err, rejected)
	}
}
