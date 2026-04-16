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

func TestRepoCompose_PostgresUsesHostGatewayWithSecureDefaultTLSMode(t *testing.T) {
	content := readRepoFile(t, "docker-compose.prod.yml")

	disallowed := []string{
		"POSTGRES_HOST: holo-postgres",
		"POSTGRES_SSLMODE: \"require\"",
		"PGHOST: holo-postgres",
		"PGSSLMODE: \"require\"",
	}
	for _, pattern := range disallowed {
		if strings.Contains(content, pattern) {
			t.Fatalf("docker-compose.prod.yml contains disallowed pattern %q", pattern)
		}
	}

	if got := strings.Count(content, "POSTGRES_HOST: host.docker.internal"); got != 6 {
		t.Fatalf("docker-compose.prod.yml POSTGRES_HOST host.docker.internal count = %d, want 6", got)
	}
	if got := strings.Count(content, "POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-require}"); got != 6 {
		t.Fatalf("docker-compose.prod.yml POSTGRES_SSLMODE secure default count = %d, want 6", got)
	}

	required := []string{
		"holo-postgres:",
		"    network_mode: host",
		"PGHOST: host.docker.internal",
		"POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-require}",
	}
	for _, pattern := range required {
		if !strings.Contains(content, pattern) {
			t.Fatalf("docker-compose.prod.yml missing required pattern %q", pattern)
		}
	}
}
