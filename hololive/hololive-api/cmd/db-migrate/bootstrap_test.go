package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// admin bootstrap이 켜진 실행에서 scraper 비밀번호가 없으면 admin 비밀번호로 scraper role을 재설정하지 않고
// 접속 전에 실패한다.
func TestBootstrapScraperRoleRequiresScraperPassword(t *testing.T) {
	t.Setenv("POSTGRES_ADMIN_PASSWORD", "admin-secret")
	t.Setenv("HOLOLIVE_SCRAPER_PASSWORD", "")
	t.Setenv("PGHOST", t.TempDir())
	t.Setenv("PGPORT", "1")
	t.Setenv("PGSSLMODE", "disable")

	err := bootstrapScraperRole(t.Context())
	if err == nil {
		t.Fatal("missing scraper password accepted")
	}

	if _, dialed := errors.AsType[*pgconn.ConnectError](err); dialed {
		t.Fatal("missing scraper password reached database connection")
	}
}

func TestBootstrapConnectionsRejectUnsafeConfigurationBeforeDial(t *testing.T) {
	paths := map[string]func(context.Context, string) error{
		"admin": func(ctx context.Context, dsn string) error {
			return ensureScraperRole(ctx, dsn, "scraper", "scraper-password", "hololive")
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

type scraperRoleRecorder struct {
	exists   bool
	commands []string
	fail     error
}

type scraperRoleRow bool

func (r scraperRoleRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("scraper role row requires one destination")
	}

	exists, ok := dest[0].(*bool)
	if !ok || exists == nil {
		return errors.New("scraper role row requires a boolean destination")
	}

	*exists = bool(r)

	return nil
}

func (r *scraperRoleRecorder) QueryRow(context.Context, string, ...any) pgx.Row {
	return scraperRoleRow(r.exists)
}

func (r *scraperRoleRecorder) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	r.commands = append(r.commands, sql)
	return pgconn.CommandTag{}, r.fail
}

func TestScraperRoleSQLUsesOnlySCRAMVerifier(t *testing.T) {
	t.Parallel()

	password := t.Name() + "'\\원문-" + t.TempDir()

	for _, exists := range []bool{false, true} {
		recorder := &scraperRoleRecorder{exists: exists}
		if err := upsertScraperRole(t.Context(), recorder, `scraper"name`, password); err != nil {
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
	recorder := &scraperRoleRecorder{fail: failure}

	if err := upsertScraperRole(t.Context(), recorder, "scraper", password); !errors.Is(err, failure) || strings.Contains(err.Error(), password) {
		t.Fatal("role failure lost cause or exposed password")
	}
}

func TestScraperRoleRejectsIncompatiblePasswordsBeforeMutation(t *testing.T) {
	t.Parallel()

	for _, password := range []string{"", "sentinel-Ｆ", "sentinel-soft\u00adhyphen", "sentinel\x00suffix", "sentinel-\xff", "sentinel-\xff\a", "sentinel-\xff\ue000"} {
		for _, exists := range []bool{false, true} {
			recorder := &scraperRoleRecorder{exists: exists}

			err := upsertScraperRole(t.Context(), recorder, "scraper", password)
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
