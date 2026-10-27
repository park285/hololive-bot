package main

import (
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
)

func TestEnvBool(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		want    bool
		wantErr string
	}{
		{name: "unset", value: "", want: false},
		{name: "true", value: "true", want: true},
		{name: "false", value: "false", want: false},
		{name: "trimmed", value: " 1 ", want: true},
		{name: "invalid", value: "enabled", wantErr: "must be a boolean"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MIGRATION_ALLOW_BLOCKING_INDEX_DROP", test.value)

			got, err := envBool("MIGRATION_ALLOW_BLOCKING_INDEX_DROP")

			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("envBool() error = %v, want containing %q", err, test.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("envBool() error = %v", err)
			}

			if got != test.want {
				t.Fatalf("envBool() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPostgresConnStringRequiresMigratorPassword(t *testing.T) {
	t.Setenv("PGPASSWORD", "")

	if _, err := postgresConnString(); err == nil || !strings.Contains(err.Error(), "PGPASSWORD") {
		t.Fatalf("postgresConnString() error = %v, want missing PGPASSWORD", err)
	}

	t.Setenv("PGPASSWORD", "migrator-secret")
	t.Setenv("PGHOST", "/var/run/postgresql")
	t.Setenv("PGSSLMODE", "disable")

	connString, err := postgresConnString()
	if err != nil {
		t.Fatalf("postgresConnString() error = %v", err)
	}

	if !strings.Contains(connString, "password='migrator-secret'") {
		t.Fatalf("postgresConnString() = %q, want migrator password", connString)
	}
}

func TestMigratorPoolRequiresExplicitValidSSLMode(t *testing.T) {
	for _, mode := range []string{"", " ", "unsupported"} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("PGPASSWORD", "migration-password-sentinel")
			t.Setenv("PGHOST", "127.0.0.1")
			t.Setenv("PGPORT", "1")
			t.Setenv("PGSSLMODE", mode)

			dsn, err := postgresConnString()
			if err != nil {
				if !strings.Contains(err.Error(), "PGSSLMODE") || strings.Contains(err.Error(), "migration-password-sentinel") {
					t.Fatalf("invalid transport diagnostic: %v", err)
				}

				return
			}

			pool, err := pgxdb.OpenPoolDSN(t.Context(), dsn, migrationPoolOptions())
			if pool != nil {
				pool.Close()
				t.Fatal("invalid sslmode unexpectedly opened a migration pool")
			}

			if err == nil || !strings.Contains(err.Error(), "sslmode") {
				t.Fatalf("expected sslmode rejection before connection, got %v", err)
			}

			if strings.Contains(err.Error(), "migration-password-sentinel") {
				t.Fatal("migration parse error exposed the password")
			}
		})
	}
}

func TestMigratorConnectionPartsPreserveExplicitSSLPolicy(t *testing.T) {
	t.Setenv("PGPASSWORD", "migration-password-sentinel")
	t.Setenv("PGHOST", "/var/run/postgresql")
	t.Setenv("PGPORT", "5432")
	t.Setenv("PGSSLMODE", " DISABLE ")
	t.Setenv("PGSSLROOTCERT", "")

	dsn, err := postgresConnString()
	if err != nil {
		t.Fatal(err)
	}

	config, err := pgxdb.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}

	if config.TLSConfig != nil {
		t.Fatal("explicit local disable policy was not normalized")
	}

	t.Setenv("PGHOST", "postgres")
	t.Setenv("PGSSLMODE", "verify-full")
	t.Setenv("PGSSLROOTCERT", "/deployment/postgres-ca.pem")

	for _, dsn := range []string{adminConnString("postgres", "admin", "test-password"), strings.Join(sslConnParts(), " ")} {
		if !strings.Contains(dsn, "sslmode='verify-full'") || !strings.Contains(dsn, "sslrootcert='/deployment/postgres-ca.pem'") {
			t.Fatal("explicit deployment certificate policy was not preserved")
		}
	}
}
