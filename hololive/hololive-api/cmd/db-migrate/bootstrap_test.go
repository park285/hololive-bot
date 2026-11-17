package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

func TestQuoteSQLLiteral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "''"},
		{name: "plain", in: "secret", want: "'secret'"},
		{name: "single quote", in: "pa'ss", want: "'pa''ss'"},
		{name: "multiple quotes", in: "a'b'c", want: "'a''b''c'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := quoteSQLLiteral(tt.in); got != tt.want {
				t.Fatalf("quoteSQLLiteral(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func setLoginRolePasswords(t *testing.T) {
	t.Helper()

	t.Setenv("POSTGRES_ADMIN_PASSWORD", "admin-secret")
	t.Setenv("PGHOST", t.TempDir())
	t.Setenv("PGPORT", "1")
	t.Setenv("PGSSLMODE", "disable")

	for _, role := range loginRoles {
		t.Setenv(role.env, role.user+"-secret")
	}
}

func TestBootstrapLoginRolesRejectUnusablePasswordsBeforeDial(t *testing.T) {
	incompatible := t.Name() + "-\uff26"
	cases := map[string]struct {
		env, value, want string
	}{
		"missing collector":    {"HOLOLIVE_COLLECTOR_C_PASSWORD", "", "HOLOLIVE_COLLECTOR_C_PASSWORD is required"},
		"missing alarm worker": {"HOLOLIVE_ALARM_WORKER_PASSWORD", " ", "HOLOLIVE_ALARM_WORKER_PASSWORD is required"},
		"missing API":          {"HOLOLIVE_DB_PASSWORD", "", "HOLOLIVE_DB_PASSWORD is required"},
		"shared with API":      {"HOLOLIVE_ALARM_WORKER_PASSWORD", runtimepolicy.PostgresRuntimeRoleUser + "-secret", "HOLOLIVE_ALARM_WORKER_PASSWORD and HOLOLIVE_DB_PASSWORD must be distinct"},
		"incompatible":         {"HOLOLIVE_DB_PASSWORD", incompatible, "HOLOLIVE_DB_PASSWORD is not a compatible PostgreSQL password"},
		"shared with admin":    {"HOLOLIVE_COLLECTOR_A_PASSWORD", "admin-secret", "POSTGRES_ADMIN_PASSWORD and HOLOLIVE_COLLECTOR_A_PASSWORD must be distinct"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setLoginRolePasswords(t)
			t.Setenv(tc.env, tc.value)

			err := bootstrapLoginRoles(t.Context())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("bootstrapLoginRoles() error = %v, want %q", err, tc.want)
			}

			if strings.Contains(err.Error(), incompatible) {
				t.Fatal("password rejection disclosed the input")
			}

			if _, dialed := errors.AsType[*pgconn.ConnectError](err); dialed {
				t.Fatal("unusable password reached database connection")
			}
		})
	}
}

func TestBootstrapRejectsEquivalentSCRAMPasswordsBeforeDial(t *testing.T) {
	const (
		plain  = "isolated equivalent password"
		mapped = "isolated\u00a0equivalent password"
	)

	if _, err := pgxdb.SCRAMVerifier(mapped); err != nil {
		t.Fatal("normalization fixture is not PostgreSQL-compatible")
	}

	for _, peer := range []string{"POSTGRES_ADMIN_PASSWORD", "HOLOLIVE_COLLECTOR_A_PASSWORD"} {
		t.Run(peer, func(t *testing.T) {
			setLoginRolePasswords(t)
			t.Setenv(peer, plain)
			t.Setenv("HOLOLIVE_DB_PASSWORD", mapped)

			err := bootstrapLoginRoles(t.Context())
			if err == nil {
				t.Fatal("equivalent SCRAM credentials were accepted")
			}

			if _, dialed := errors.AsType[*pgconn.ConnectError](err); dialed {
				t.Fatal("equivalent SCRAM credentials reached the database")
			}
		})
	}
}

func TestBootstrapConnectionsRejectUnsafeConfigurationBeforeDial(t *testing.T) {
	paths := map[string]func(context.Context, string) error{
		"admin": func(ctx context.Context, dsn string) error {
			return ensureLoginRoles(ctx, dsn, "scraper", []loginCredential{
				{user: "collector", connectionLimit: 7, collector: true, password: "collector-password"},
			}, "hololive")
		},
		"database": func(ctx context.Context, dsn string) error {
			return grantScraperSchemaUsage(ctx, dsn, "scraper")
		},
	}
	for name, connect := range paths {
		t.Run(name, func(t *testing.T) {
			for _, dsn := range []string{
				"host=127.0.0.1 port=1 user=admin password=bootstrap-password-sentinel",
				"host=127.0.0.1 port=1 user=admin password=bootstrap-password-sentinel sslmode=unsupported",
				"host=127.0.0.1 port=invalid user=admin password=bootstrap-password-sentinel sslmode=disable",
			} {
				err := connect(t.Context(), dsn)
				if err == nil {
					t.Fatal("unsafe database configuration accepted")
				}

				if _, dialed := errors.AsType[*pgconn.ConnectError](err); dialed {
					t.Fatal("unsafe database configuration reached connection")
				}

				if strings.Contains(err.Error(), "bootstrap-password-sentinel") {
					t.Fatal("bootstrap configuration error exposed the password")
				}
			}
		})
	}
}

func TestScraperGroupRoleIsNoLogin(t *testing.T) {
	t.Parallel()

	for _, exists := range []bool{false, true} {
		recorder := &roleRecorder{exists: exists}
		if err := upsertScraperGroupRole(t.Context(), recorder, `scraper"group`); err != nil {
			t.Fatal(err)
		}

		want := 3

		if exists {
			want = 2
		}

		if len(recorder.commands) != want {
			t.Fatalf("commands=%d want=%d", len(recorder.commands), want)
		}

		roleCommands, terminate := recorder.commands[:want-1], recorder.commands[want-1]
		for _, command := range roleCommands {
			if !strings.Contains(command, "NOLOGIN") || !strings.Contains(command, `"scraper""group"`) {
				t.Fatal("scraper group SQL lost no-login or identifier quoting")
			}
		}

		if !strings.Contains(terminate, "pg_terminate_backend") {
			t.Fatal("scraper group sessions were not terminated after NOLOGIN")
		}
	}
}

type roleRecorder struct {
	exists   bool
	commands []string
	fail     error
}

type scraperRoleRow bool

func (r scraperRoleRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("scraper role row requires one destination")
	}

	switch value := dest[0].(type) {
	case *bool:
		*value = bool(r)
	case *string:
		*value = ""
	default:
		return errors.New("unexpected role row destination")
	}

	return nil
}

func (r *roleRecorder) QueryRow(context.Context, string, ...any) pgx.Row {
	return scraperRoleRow(r.exists)
}

func (r *roleRecorder) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	r.commands = append(r.commands, sql)
	return pgconn.CommandTag{}, r.fail
}

func TestScraperRoleSQLUsesOnlySCRAMVerifier(t *testing.T) {
	t.Parallel()

	password := t.Name() + "'\\원문-" + t.TempDir()

	for _, exists := range []bool{false, true} {
		recorder := &roleRecorder{exists: exists}
		if err := upsertLoginRole(t.Context(), recorder, `scraper"name`, password); err != nil {
			t.Fatal(err)
		}

		want := 2

		if exists {
			want = 1
		}

		if len(recorder.commands) != want {
			t.Fatalf("commands=%d want=%d", len(recorder.commands), want)
		}

		for _, command := range recorder.commands {
			if strings.Contains(command, password) || strings.Contains(command, quoteSQLLiteral(password)) {
				t.Fatal("role SQL exposed plaintext password")
			}

			if !strings.Contains(command, `'SCRAM-SHA-256$4096:`) || !strings.Contains(command, `"scraper""name"`) {
				t.Fatal("role SQL lost verifier or identifier quoting")
			}
		}
	}

	failure := errors.New("statement rejected")
	recorder := &roleRecorder{fail: failure}

	if err := upsertLoginRole(t.Context(), recorder, "scraper", password); err == nil || !errors.Is(err, failure) || strings.Contains(err.Error(), password) {
		t.Fatal("role failure lost cause or exposed password")
	}
}

func TestScraperRoleRejectsIncompatiblePasswordsBeforeMutation(t *testing.T) {
	t.Parallel()

	for _, password := range []string{"", "sentinel-Ｆ", "sentinel-soft\u00adhyphen", "sentinel\x00suffix", "sentinel-\xff", "sentinel-\xff\a", "sentinel-\xff\ue000"} {
		for _, exists := range []bool{false, true} {
			recorder := &roleRecorder{exists: exists}

			err := upsertLoginRole(t.Context(), recorder, "scraper", password)
			if err == nil {
				t.Fatal("expected password compatibility rejection")
			}

			if password != "" && strings.Contains(err.Error(), password) {
				t.Fatal("password compatibility rejection disclosed input")
			}

			if len(recorder.commands) != 0 {
				t.Fatal("incompatible password mutated the role")
			}
		}
	}
}

func TestPrivilegedTransportRequiresSocketOrVerifiedTLS(t *testing.T) {
	for _, test := range []struct {
		host, mode string
		valid      bool
	}{
		{"/var/run/postgresql", "disable", true},
		{"postgres", "verify-full", true},
		{"postgres", "disable", false},
		{"localhost", "require", false},
		{"/var/run/postgresql", "verify-full", false},
		{"/var/run/postgresql,postgres", "disable", false},
	} {
		t.Setenv("PGHOST", test.host)
		t.Setenv("PGSSLMODE", test.mode)

		if err := validatePrivilegedTransport(); (err == nil) != test.valid {
			t.Fatalf("host=%s mode=%s error=%v", test.host, test.mode, err)
		}
	}
}

func TestBootstrapLoginRolesSkipsPureMigration(t *testing.T) {
	t.Setenv("POSTGRES_ADMIN_PASSWORD", "")
	t.Setenv("PGPASSWORD", "migrator-fixture-password")
	t.Setenv("PGHOST", t.TempDir())
	t.Setenv("PGPORT", "1")

	for _, role := range loginRoles {
		t.Setenv(role.env, "")
	}

	if err := bootstrapLoginRoles(t.Context()); err != nil {
		t.Fatalf("pure migration attempted privileged bootstrap: %v", err)
	}
}

func TestStoredSCRAMRequiresServerKeyAndBoundedCost(t *testing.T) {
	t.Parallel()

	const password = "fixture-authentication-password"

	verifier, err := pgxdb.SCRAMVerifier(password)
	if err != nil {
		t.Fatal(err)
	}

	var invalidServerKey [32]byte

	tampered := verifier[:strings.LastIndexByte(verifier, ':')+1] + base64.StdEncoding.EncodeToString(invalidServerKey[:])

	if matches, err := matchesValidatedSCRAMPassword(password, tampered); err != nil || matches {
		t.Fatalf("invalid server signature key was reusable: match=%t err=%v", matches, err)
	}

	excessive := strings.Replace(verifier, "$4096:", "$10000001:", 1)
	if _, err := matchesValidatedSCRAMPassword(password, excessive); err == nil {
		t.Fatal("stored SCRAM cost exceeded the client authentication bound")
	}
}
