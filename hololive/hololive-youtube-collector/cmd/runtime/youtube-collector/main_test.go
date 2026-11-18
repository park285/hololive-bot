package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestYouTubeCollectorLogFileNameUsesExplicitEnv(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "youtube-collector-central.log")

	if got, err := youtubeCollectorLogFileName(); err != nil || got != "youtube-collector-central.log" {
		t.Fatalf("youtubeCollectorLogFileName() = (%q, %v), want youtube-collector-central.log", got, err)
	}
}

func TestYouTubeCollectorLogFileNameDefaultsWhenUnset(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "")

	if got, err := youtubeCollectorLogFileName(); err != nil || got != "youtube-collector.log" {
		t.Fatalf("youtubeCollectorLogFileName() = (%q, %v), want youtube-collector.log", got, err)
	}
}

func TestYouTubeCollectorLogFileNameRejectsPathSeparators(t *testing.T) {
	for _, name := range []string{"logs/youtube-collector.log", `logs\youtube-collector.log`} {
		t.Setenv("YOUTUBE_COLLECTOR_LOG_FILE_NAME", name)

		if got, err := youtubeCollectorLogFileName(); err == nil {
			t.Fatalf("youtubeCollectorLogFileName(%q) = %q, want startup error", name, got)
		}
	}
}

func TestPooledEndpointAllowed(t *testing.T) {
	for _, tc := range []struct {
		instance, user, host, ca string
		port                     int
	}{
		{"youtube-collector-a", "hololive_collector_a", "100.100.1.8", "/etc/stack-secrets/hololive-bot/certs/postgres-ca.pem", 6433},
		{"youtube-collector-b", "hololive_collector_b", "100.100.1.8", "/run/hololive-bot/certs/postgres-ca.pem", 6433},
		{"youtube-collector-c", "hololive_collector_c", "holo-pgbouncer", "/run/hololive-bot/certs/postgres-ca.pem", 6432},
		{"youtube-collector-d", "hololive_collector_d", "100.100.1.8", "/run/hololive-bot/certs/postgres-ca.pem", 6433},
	} {
		t.Run(tc.instance, func(t *testing.T) {
			db := settings.PostgresConfig{
				Host: tc.host, Port: tc.port, User: tc.user, Database: "hololive", Password: "fixture",
				SSLMode: "verify-full", SSLRootCert: tc.ca, QueryExecMode: "cache_statement",
			}
			if !pooledEndpointAllowed(tc.instance, &db) {
				t.Fatal("rejected collector's pooled configuration")
			}

			for name, change := range map[string]func(*settings.PostgresConfig){
				"host":     func(p *settings.PostgresConfig) { p.Host = "holo-postgres" },
				"port":     func(p *settings.PostgresConfig) { p.Port = 5433 },
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

					if pooledEndpointAllowed(tc.instance, &invalid) {
						t.Fatal("accepted invalid pooled configuration")
					}
				})
			}
		})
	}
}

func TestPooledDatabaseCheckRejectsLibpqOverrideWithoutDisclosure(t *testing.T) {
	const marker = "test-only-sensitive-value"

	t.Setenv("PGSERVICEFILE", marker)
	t.Setenv("POSTGRES_PASSWORD", marker)

	err := checkPooledDatabase(t.Context())
	if !errors.Is(err, errUnexpectedLibpqEnv) {
		t.Fatal("libpq override was not rejected before configuration or connection")
	}

	if err != nil && strings.Contains(err.Error(), marker) {
		t.Fatal("database check disclosed a service setting")
	}
}
