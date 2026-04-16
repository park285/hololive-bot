package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRootFromConfigTest(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

func readRepoFile(t *testing.T, relativePath string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(repoRootFromConfigTest(t), relativePath))
	if err != nil {
		t.Fatalf("read %s failed: %v", relativePath, err)
	}

	return string(content)
}

func TestRepoEnvExample_DefaultsToProductionAppEnv(t *testing.T) {
	content := readRepoFile(t, ".env.example")

	if !strings.Contains(content, "APP_ENV=production") {
		t.Fatalf(".env.example missing APP_ENV=production")
	}
	if strings.Contains(content, "APP_ENV=development") {
		t.Fatalf(".env.example still contains APP_ENV=development")
	}
}

func TestRepoCompose_PostgresUsesInternalNetworkAndPinnedTLSMode(t *testing.T) {
	content := readRepoFile(t, "docker-compose.prod.yml")

	disallowed := []string{
		"network_mode: host",
		"POSTGRES_HOST: host.docker.internal",
		"POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-require}",
		"PGHOST: host.docker.internal",
	}
	for _, pattern := range disallowed {
		if strings.Contains(content, pattern) {
			t.Fatalf("docker-compose.prod.yml contains disallowed pattern %q", pattern)
		}
	}

	if got := strings.Count(content, "POSTGRES_HOST: holo-postgres"); got != 6 {
		t.Fatalf("docker-compose.prod.yml POSTGRES_HOST holo-postgres count = %d, want 6", got)
	}
	if got := strings.Count(content, "POSTGRES_SSLMODE: \"require\""); got != 6 {
		t.Fatalf("docker-compose.prod.yml POSTGRES_SSLMODE require count = %d, want 6", got)
	}

	required := []string{
		"holo-postgres:",
		"    networks:\n      - hololive-net",
		"PGHOST: holo-postgres",
		"PGSSLMODE: \"require\"",
	}
	for _, pattern := range required {
		if !strings.Contains(content, pattern) {
			t.Fatalf("docker-compose.prod.yml missing required pattern %q", pattern)
		}
	}
}
