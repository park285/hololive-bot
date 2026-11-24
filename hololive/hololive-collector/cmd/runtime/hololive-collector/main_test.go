package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestYouTubeCollectorLogFileNameUsesExplicitEnv(t *testing.T) {
	t.Setenv("COLLECTOR_LOG_FILE_NAME", "collector-central.log")

	if got, err := youtubeCollectorLogFileName(); err != nil || got != "collector-central.log" {
		t.Fatalf("youtubeCollectorLogFileName() = (%q, %v), want collector-central.log", got, err)
	}
}

func TestYouTubeCollectorLogFileNameDefaultsWhenUnset(t *testing.T) {
	t.Setenv("COLLECTOR_LOG_FILE_NAME", "")

	if got, err := youtubeCollectorLogFileName(); err != nil || got != "collector.log" {
		t.Fatalf("youtubeCollectorLogFileName() = (%q, %v), want collector.log", got, err)
	}
}

func TestYouTubeCollectorLogFileNameRejectsPathSeparators(t *testing.T) {
	for _, name := range []string{"logs/collector.log", `logs\collector.log`} {
		t.Setenv("COLLECTOR_LOG_FILE_NAME", name)

		if got, err := youtubeCollectorLogFileName(); err == nil {
			t.Fatalf("youtubeCollectorLogFileName(%q) = %q, want startup error", name, got)
		}
	}
}

func TestDatabaseEndpointAllowed(t *testing.T) {
	for _, tc := range []struct {
		instance, user, host, ca string
		port                     int
	}{
		{"collector-a", "hololive_collector_a", "100.100.1.8", "/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem", 5433},
		{"collector-b", "hololive_collector_b", "100.100.1.8", "/run/hololive-bot/certs/postgres-ca.pem", 5433},
		{"collector-c", "hololive_collector_c", "holo-postgres", "/run/hololive-bot/certs/postgres-ca.pem", 5432},
		{"collector-d", "hololive_collector_d", "100.100.1.8", "/run/hololive-bot/certs/postgres-ca.pem", 5433},
	} {
		t.Run(tc.instance, func(t *testing.T) {
			db := settings.PostgresConfig{
				Host: tc.host, Port: tc.port, User: tc.user, Database: "hololive", Password: "fixture",
				SSLMode: "verify-full", SSLRootCert: tc.ca, QueryExecMode: "cache_statement",
			}
			if !databaseEndpointAllowed(tc.instance, &db) {
				t.Fatal("rejected collector's database configuration")
			}

			for name, change := range map[string]func(*settings.PostgresConfig){
				"host":     func(p *settings.PostgresConfig) { p.Host = "holo-pgbouncer" },
				"port":     func(p *settings.PostgresConfig) { p.Port = 6433 },
				"role":     func(p *settings.PostgresConfig) { p.User = "hololive_runtime" },
				"database": func(p *settings.PostgresConfig) { p.Database = "postgres" },
				"socket":   func(p *settings.PostgresConfig) { p.SocketPath = "/run/postgresql" },
				"password": func(p *settings.PostgresConfig) { p.Password = "" },
				"tls":      func(p *settings.PostgresConfig) { p.SSLMode = "require" },
				"ca":       func(p *settings.PostgresConfig) { p.SSLRootCert = "system" },
				"protocol": func(p *settings.PostgresConfig) { p.QueryExecMode = "simple_protocol" },
			} {
				t.Run(name, func(t *testing.T) {
					invalid := db
					change(&invalid)

					if databaseEndpointAllowed(tc.instance, &invalid) {
						t.Fatal("accepted invalid database configuration")
					}
				})
			}
		})
	}
}

func TestDatabaseEndpointCheckRejectsLibpqOverrideWithoutDisclosure(t *testing.T) {
	const marker = "test-only-sensitive-value"

	t.Setenv("PGSERVICEFILE", marker)
	t.Setenv("POSTGRES_PASSWORD", marker)

	err := checkDatabaseEndpoint(t.Context())
	if !errors.Is(err, errUnexpectedLibpqEnv) {
		t.Fatal("libpq override was not rejected before configuration or connection")
	}

	if err != nil && strings.Contains(err.Error(), marker) {
		t.Fatal("database check disclosed a service setting")
	}
}
