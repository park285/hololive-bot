package main

import (
	"strings"
	"testing"
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
	t.Setenv("PGHOST", "127.0.0.1")
	t.Setenv("PGPORT", "1")
	t.Setenv("PGSSLMODE", "disable")

	err := bootstrapScraperRole(t.Context())
	if err == nil || !strings.Contains(err.Error(), "HOLOLIVE_SCRAPER_PASSWORD is required") {
		t.Fatalf("bootstrapScraperRole() error = %v, want missing HOLOLIVE_SCRAPER_PASSWORD", err)
	}
}
