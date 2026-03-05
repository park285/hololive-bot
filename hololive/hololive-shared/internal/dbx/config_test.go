package dbx

import (
	"strings"
	"testing"
	"time"
)

func TestSafeDSN(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		wantSafe string
	}{
		{
			name: "TCP with password masked",
			cfg: Config{
				Host:     "localhost",
				Port:     5432,
				User:     "admin",
				Password: "x",
				Name:     "mydb",
				SSLMode:  "disable",
			},
			wantSafe: "host=localhost port=5432 user=admin password=*** dbname=mydb sslmode=disable",
		},
		{
			name: "UDS with password masked",
			cfg: Config{
				SocketPath: "/var/run/postgresql",
				User:       "admin",
				Password:   "x",
				Name:       "mydb",
				SSLMode:    "disable",
			},
			wantSafe: "host=/var/run/postgresql user=admin password=*** dbname=mydb sslmode=disable",
		},
		{
			name: "empty password stays empty",
			cfg: Config{
				Host:     "localhost",
				Port:     5432,
				User:     "admin",
				Password: "",
				Name:     "mydb",
				SSLMode:  "disable",
			},
			wantSafe: "host=localhost port=5432 user=admin password= dbname=mydb sslmode=disable",
		},
		{
			name: "password not in SafeDSN output",
			cfg: Config{
				Host:     "db.prod",
				Port:     5432,
				User:     "app",
				Password: "z",
				Name:     "prod",
			},
			wantSafe: "host=db.prod port=5432 user=app password=*** dbname=prod sslmode=require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			safe := tt.cfg.SafeDSN()
			if safe != tt.wantSafe {
				t.Errorf("SafeDSN() = %q, want %q", safe, tt.wantSafe)
			}
			// DSN은 원본 비밀번호 유지 확인
			if tt.cfg.Password != "" {
				dsn := tt.cfg.DSN()
				if dsn == safe {
					t.Error("DSN() should differ from SafeDSN() when password is set")
				}
				if !strings.Contains(dsn, tt.cfg.Password) {
					t.Error("DSN() should contain the original password")
				}
				if strings.Contains(safe, tt.cfg.Password) {
					t.Errorf("SafeDSN() must not contain the original password %q", tt.cfg.Password)
				}
			}
		})
	}
}

func TestDefaultPoolConfig(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		wantMin     int
		wantMax     int
		wantMaxIdle int
	}{
		{
			name:        "default values when no env vars set",
			envVars:     nil,
			wantMin:     5,
			wantMax:     20,
			wantMaxIdle: 0,
		},
		{
			name: "custom values from env vars",
			envVars: map[string]string{
				"DB_POOL_MIN_CONNS":      "5",
				"DB_POOL_MAX_CONNS":      "25",
				"DB_POOL_MAX_IDLE_CONNS": "3",
			},
			wantMin:     5,
			wantMax:     25,
			wantMaxIdle: 3,
		},
		{
			name: "clamp min below lower bound",
			envVars: map[string]string{
				"DB_POOL_MIN_CONNS": "0",
			},
			wantMin:     1, // clamped to 1
			wantMax:     20,
			wantMaxIdle: 0,
		},
		{
			name: "clamp min above upper bound",
			envVars: map[string]string{
				"DB_POOL_MIN_CONNS": "150",
			},
			wantMin:     100, // clamped to 100
			wantMax:     20,
			wantMaxIdle: 0,
		},
		{
			name: "clamp max below lower bound",
			envVars: map[string]string{
				"DB_POOL_MAX_CONNS": "0",
			},
			wantMin:     5,
			wantMax:     1, // clamped to 1
			wantMaxIdle: 0,
		},
		{
			name: "clamp max above upper bound",
			envVars: map[string]string{
				"DB_POOL_MAX_CONNS": "300",
			},
			wantMin:     5,
			wantMax:     200, // clamped to 200
			wantMaxIdle: 0,
		},
		{
			name: "invalid env var returns default",
			envVars: map[string]string{
				"DB_POOL_MIN_CONNS": "invalid",
				"DB_POOL_MAX_CONNS": "abc",
			},
			wantMin:     5,  // default
			wantMax:     20, // default
			wantMaxIdle: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear and set env vars
			for _, key := range []string{"DB_POOL_MIN_CONNS", "DB_POOL_MAX_CONNS", "DB_POOL_MAX_IDLE_CONNS"} {
				t.Setenv(key, "")
			}
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg := DefaultPoolConfig()

			if cfg.MinConns != tt.wantMin {
				t.Errorf("MinConns = %d, want %d", cfg.MinConns, tt.wantMin)
			}
			if cfg.MaxConns != tt.wantMax {
				t.Errorf("MaxConns = %d, want %d", cfg.MaxConns, tt.wantMax)
			}
			if cfg.MaxIdleConns != tt.wantMaxIdle {
				t.Errorf("MaxIdleConns = %d, want %d", cfg.MaxIdleConns, tt.wantMaxIdle)
			}
			if cfg.ConnMaxLifetime != time.Hour {
				t.Errorf("ConnMaxLifetime = %v, want %v", cfg.ConnMaxLifetime, time.Hour)
			}
			if cfg.ConnMaxIdleTime != 30*time.Minute {
				t.Errorf("ConnMaxIdleTime = %v, want %v", cfg.ConnMaxIdleTime, 30*time.Minute)
			}
		})
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		min      int
		max      int
		expected int
	}{
		{"within range", 5, 1, 10, 5},
		{"at min", 1, 1, 10, 1},
		{"at max", 10, 1, 10, 10},
		{"below min", 0, 1, 10, 1},
		{"above max", 15, 1, 10, 10},
		{"negative value", -5, 0, 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clamp(tt.value, tt.min, tt.max)
			if result != tt.expected {
				t.Errorf("clamp(%d, %d, %d) = %d, want %d", tt.value, tt.min, tt.max, result, tt.expected)
			}
		})
	}
}

func TestNormalizeQueryExecMode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "cache_statement", in: "cache_statement", want: "cache_statement"},
		{name: "upper_with_spaces", in: "  EXEC  ", want: "exec"},
		{name: "simple_protocol", in: "simple_protocol", want: "simple_protocol"},
		{name: "invalid", in: "something_else", want: ""},
		{name: "empty", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeQueryExecMode(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeQueryExecMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
