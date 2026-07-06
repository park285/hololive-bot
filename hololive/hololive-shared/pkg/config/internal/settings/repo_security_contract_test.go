package settings

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func repoRootFromConfigTest(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", ".."))
}

func readRepoFile(t *testing.T, relativePath string) string {
	t.Helper()

	content, err := fs.ReadFile(os.DirFS(repoRootFromConfigTest(t)), repoLocalPath(t, relativePath))
	if err != nil {
		t.Fatalf("read %s failed: %v", relativePath, err)
	}

	return string(content)
}

func repoLocalPath(t *testing.T, relativePath string) string {
	t.Helper()

	if filepath.IsAbs(relativePath) {
		t.Fatalf("repo path %q must be relative", relativePath)
	}
	clean := filepath.Clean(relativePath)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		t.Fatalf("repo path %q escapes repo root", relativePath)
	}
	return filepath.ToSlash(clean)
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

func TestRepoComposeProdHardenedDefaults(t *testing.T) {
	content := readRepoFile(t, "deploy/compose/docker-compose.prod.yml")

	assertProdComposeDisallowedPatterns(t, content)
	assertProdComposeRequiredPatterns(t, content)
	assertProdComposeEgressEnvFiles(t, content)
	assertProdComposeNonEgressIsolation(t, content)
}

func assertProdComposeDisallowedPatterns(t *testing.T, content string) {
	t.Helper()

	disallowed := []string{
		"100.100.1.3",
		"${VALKEY_PORT_BIND_IP:-100.100.1.3}:6379:6379",
		"${ADMIN_DASHBOARD_PORT_BIND_IP:-100.100.1.3}:30190:30190",
		"${HOLOLIVE_BOT_PORT_BIND_IP:-100.100.1.3}:30001:30001",
		"network_mode: host",
		"/run/hololive-bot/certs:/run/hololive-bot/certs:ro",
		"POSTGRES_HOST: host.docker.internal",
		"POSTGRES_PORT: \"5433\"",
		"POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-require}",
		"IRIS_BASE_URL_FILE: ${IRIS_BASE_URL_FILE:-/app/runtime-config/iris_base_url}",
		"http://100.100.1.3:30190",
		"PGSSLMODE: \"require\"",
	}
	for _, pattern := range disallowed {
		if strings.Contains(content, pattern) {
			t.Fatalf("docker-compose.prod.yml contains disallowed pattern %q", pattern)
		}
	}

	bindIPDefault := regexp.MustCompile(`\$\{[A-Z0-9_]*_BIND_IP:-100\.100\.1\.3\}`)
	if match := bindIPDefault.FindString(content); match != "" {
		t.Fatalf("docker-compose.prod.yml contains Tailnet bind default %q", match)
	}
}

func assertProdComposeRequiredPatterns(t *testing.T, content string) {
	t.Helper()

	if got := strings.Count(content, "POSTGRES_HOST: holo-postgres"); got != 1 {
		t.Fatalf("docker-compose.prod.yml POSTGRES_HOST holo-postgres anchor count = %d, want 1", got)
	}
	if got := strings.Count(content, "POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-verify-full}"); got != 1 {
		t.Fatalf("docker-compose.prod.yml POSTGRES_SSLMODE verify-full default count = %d, want 1", got)
	}
	if got := strings.Count(content, "*postgres-env"); got != 3 {
		t.Fatalf("docker-compose.prod.yml postgres env anchor usage count = %d, want 3", got)
	}

	required := []string{
		"holo-postgres:",
		"    networks:",
		"x-postgres-env: &postgres-env",
		"  POSTGRES_PORT: \"5432\"",
		"  POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-verify-full}",
		"  IRIS_BASE_URL_FILE: ${IRIS_BASE_URL_FILE:-}",
		"--unixsocketperm 777",
	}
	for _, pattern := range required {
		if !strings.Contains(content, pattern) {
			t.Fatalf("docker-compose.prod.yml missing required pattern %q", pattern)
		}
	}

	appAnchor := topLevelYAMLBlock(t, content, "x-app-service:")
	if strings.Contains(appAnchor, "env_file:") {
		t.Fatalf("x-app-service still defines env_file")
	}
}

func assertProdComposeEgressEnvFiles(t *testing.T, content string) {
	t.Helper()

	egressOwners := []string{"hololive-api", "hololive-alarm-worker"}
	for _, service := range egressOwners {
		block := composeServiceBlock(t, content, service)
		wantEnvFile := map[string]string{
			"hololive-api":          "${HOLOLIVE_API_ENV_FILE:-/run/hololive-bot/bot.env}",
			"hololive-alarm-worker": "${HOLOLIVE_ALARM_WORKER_ENV_FILE:-/run/hololive-bot/alarm-worker.env}",
		}[service]
		if !strings.Contains(block, "env_file:") || !strings.Contains(block, wantEnvFile) {
			t.Fatalf("%s must use per-service env_file %q for app-only secrets", service, wantEnvFile)
		}
		if strings.Contains(block, "/run/hololive-bot/env") || strings.Contains(block, "COMPOSE_ENV_FILE") {
			t.Fatalf("%s must not consume monolithic COMPOSE_ENV_FILE as env_file", service)
		}
		if !strings.Contains(block, "*iris-env") {
			t.Fatalf("%s must keep x-iris-env", service)
		}
	}
}

func assertProdComposeNonEgressIsolation(t *testing.T, content string) {
	t.Helper()

	nonEgress := []string{"youtube-producer", "admin-dashboard"}
	for _, service := range nonEgress {
		block := composeServiceBlock(t, content, service)
		assertNonEgressEnvFilePolicy(t, service, block)
		for _, pattern := range []string{"*iris-env", "IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"} {
			if strings.Contains(block, pattern) {
				t.Fatalf("%s contains Iris egress pattern %q", service, pattern)
			}
		}
		if service != "admin-dashboard" {
			for _, key := range []string{"ADMIN_PASS_BCRYPT", "ADMIN_PASS_HASH", "ADMIN_SECRET_KEY", "SESSION_SECRET"} {
				if strings.Contains(block, key) {
					t.Fatalf("%s contains dashboard-only secret %q", service, key)
				}
			}
		}
	}
}

func assertNonEgressEnvFilePolicy(t *testing.T, service, block string) {
	t.Helper()

	if service != "admin-dashboard" {
		if strings.Contains(block, "env_file:") {
			t.Fatalf("%s must not define env_file in hardened docker-compose.prod.yml", service)
		}
		return
	}
	if !strings.Contains(block, "${ADMIN_DASHBOARD_ENV_FILE:-/run/hololive-bot/admin-dashboard.env}") {
		t.Fatalf("admin-dashboard must inject its secrets via the scoped admin-dashboard.env env_file")
	}
	if strings.Contains(block, "/run/hololive-bot/env") || strings.Contains(block, "COMPOSE_ENV_FILE") {
		t.Fatalf("admin-dashboard must not consume monolithic COMPOSE_ENV_FILE as env_file")
	}
}

func TestRepoComposeProdRenderedIsolation(t *testing.T) {
	cfg := renderComposeConfig(t, "deploy/compose/docker-compose.prod.yml")

	assertProdRenderedPostgresIsolation(t, cfg)
	assertProdRenderedNonEgressSecretIsolation(t, cfg)
	assertProdRenderedEgressRuntimeKeys(t, cfg)
	assertProdRenderedScopedProducerKeys(t, cfg)
	assertProdRenderedNoRuntimeConfigMount(t, cfg)
	assertProdRenderedPortAndCertScope(t, cfg)
}

func assertProdRenderedPostgresIsolation(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{"holo-postgres", "hololive-db-migrate"} {
		if got := stringValue(composeService(t, cfg, service)["network_mode"]); got == "host" {
			t.Fatalf("%s rendered with network_mode=host", service)
		}
	}

	for _, service := range []string{"hololive-api", "hololive-alarm-worker", "youtube-producer"} {
		env := composeEnvironment(t, cfg, service)
		if env["POSTGRES_HOST"] != "holo-postgres" {
			t.Fatalf("%s POSTGRES_HOST = %q, want holo-postgres", service, env["POSTGRES_HOST"])
		}
		if env["POSTGRES_PORT"] != "5432" {
			t.Fatalf("%s POSTGRES_PORT = %q, want 5432", service, env["POSTGRES_PORT"])
		}
		if env["POSTGRES_SSLMODE"] != "verify-full" {
			t.Fatalf("%s POSTGRES_SSLMODE = %q, want verify-full", service, env["POSTGRES_SSLMODE"])
		}
	}
}

func assertProdRenderedNonEgressSecretIsolation(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{"youtube-producer", "admin-dashboard"} {
		env := composeEnvironment(t, cfg, service)
		for _, key := range []string{"IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"} {
			if _, ok := env[key]; ok {
				t.Fatalf("%s rendered with %s", service, key)
			}
		}
		if service != "admin-dashboard" {
			for _, key := range []string{"ADMIN_PASS_BCRYPT", "ADMIN_PASS_HASH", "ADMIN_SECRET_KEY", "SESSION_SECRET"} {
				if _, ok := env[key]; ok {
					t.Fatalf("%s rendered with dashboard-only secret %s", service, key)
				}
			}
		}
	}
}

func assertProdRenderedEgressRuntimeKeys(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{"hololive-api", "hololive-alarm-worker"} {
		env := composeEnvironment(t, cfg, service)
		for _, key := range []string{
			"KAKAO_ROOMS",
			"KAKAO_ACL_ENABLED",
			"KAKAO_ACL_MODE",
			"HOLODEX_API_KEY",
			"HOLODEX_API_KEY_1",
		} {
			if _, ok := env[key]; !ok {
				t.Fatalf("%s missing egress runtime key %s", service, key)
			}
		}
		if env["API_SECRET_KEY"] != "dummy" {
			t.Fatalf("%s API_SECRET_KEY = %q, want scoped env_file value", service, env["API_SECRET_KEY"])
		}
	}
}

func assertProdRenderedScopedProducerKeys(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{"youtube-producer"} {
		env := composeEnvironment(t, cfg, service)
		if _, ok := env["API_SECRET_KEY"]; !ok {
			t.Fatalf("%s missing scoped API_SECRET_KEY mapping", service)
		}
	}

	producerEnv := composeEnvironment(t, cfg, "youtube-producer")
	for _, key := range []string{"HOLODEX_API_KEY", "HOLODEX_API_KEY_1"} {
		if _, ok := producerEnv[key]; !ok {
			t.Fatalf("youtube-producer missing scoped %s mapping", key)
		}
	}
	if producerEnv["HOLOLIVE_HTTP_TRANSPORTS"] != "h3" {
		t.Fatalf("youtube-producer HOLOLIVE_HTTP_TRANSPORTS = %q, want h3", producerEnv["HOLOLIVE_HTTP_TRANSPORTS"])
	}
}

func assertProdRenderedNoRuntimeConfigMount(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{"hololive-api", "hololive-alarm-worker"} {
		env := composeEnvironment(t, cfg, service)
		if env["IRIS_BASE_URL_FILE"] != "" {
			t.Fatalf("%s IRIS_BASE_URL_FILE = %q, want empty default", service, env["IRIS_BASE_URL_FILE"])
		}
	}

	for _, service := range []string{"youtube-producer", "admin-dashboard"} {
		for _, target := range composeVolumeTargets(t, cfg, service) {
			if target == "/app/runtime-config" {
				t.Fatalf("%s still mounts runtime-config", service)
			}
		}
	}
}

func assertProdRenderedPortAndCertScope(t *testing.T, cfg renderedCompose) {
	t.Helper()

	h3KeyConsumers := map[string]bool{
		"hololive-api":          true,
		"hololive-alarm-worker": true,
		"youtube-producer":      true,
	}
	for serviceName, service := range cfg.Services {
		for _, port := range composePorts(t, serviceName, service) {
			if port.HostIP != "" && port.HostIP != "127.0.0.1" && port.HostIP != "::1" && port.HostIP != "localhost" {
				t.Fatalf("%s publishes non-loopback port %+v", serviceName, port)
			}
		}
		for _, target := range composeVolumeTargets(t, cfg, serviceName) {
			if target == "/run/hololive-bot/certs" {
				t.Fatalf("%s still mounts the broad cert directory", serviceName)
			}
			if strings.HasSuffix(target, ".key") && !h3KeyConsumers[serviceName] {
				t.Fatalf("%s mounts private key file %s", serviceName, target)
			}
		}
	}
}

func TestRepoComposeAPCertMountsAreMinimized(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{
			name: "osaka",
			file: "deploy/compose/docker-compose.osaka.yml",
		},
		{
			name: "osaka2",
			file: "deploy/compose/docker-compose.osaka2.yml",
		},
		{
			name: "seoul",
			file: "deploy/compose/docker-compose.seoul.yml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := renderAPComposeConfig(t, "deploy/compose/docker-compose.prod.yml", renderableAPComposeFile(t, tt.file))
			assertAPComposeCertMountsAreMinimized(t, cfg, tt.file)
			assertAPComposeDoesNotRequireCentralEgressEnvFiles(t, cfg, tt.file)
		})
	}
}

func TestRepoComposeLiveCompatOverlayRestoresLiveWiringWithScopedNonEgress(t *testing.T) {
	overlay := readRepoFile(t, "deploy/compose/docker-compose.live-compat.yml")
	assertLiveCompatOverlayText(t, overlay)

	cfg := renderComposeConfig(t, "deploy/compose/docker-compose.prod.yml", "deploy/compose/docker-compose.live-compat.yml")

	assertLiveCompatRenderedPortsAndModes(t, cfg)
	assertLiveCompatRenderedPostgres(t, cfg)
	assertLiveCompatRenderedSecrets(t, cfg)
	assertLiveCompatRenderedRuntimeConfig(t, cfg)
}

func assertLiveCompatOverlayText(t *testing.T, overlay string) {
	t.Helper()

	for _, service := range []string{"hololive-api", "hololive-alarm-worker"} {
		block := composeServiceBlock(t, overlay, service)
		wantEnvFile := map[string]string{
			"hololive-api":          "${HOLOLIVE_API_ENV_FILE:-/run/hololive-bot/bot.env}",
			"hololive-alarm-worker": "${HOLOLIVE_ALARM_WORKER_ENV_FILE:-/run/hololive-bot/alarm-worker.env}",
		}[service]
		if !strings.Contains(block, "env_file:") || !strings.Contains(block, wantEnvFile) {
			t.Fatalf("live overlay must keep per-service env_file %q for %s", wantEnvFile, service)
		}
		if strings.Contains(block, "/run/hololive-bot/env") || strings.Contains(block, "COMPOSE_ENV_FILE") {
			t.Fatalf("live overlay must not restore monolithic env_file for %s", service)
		}
	}
	for _, service := range []string{"youtube-producer", "admin-dashboard"} {
		block := composeServiceBlock(t, overlay, service)
		if strings.Contains(block, "env_file:") {
			t.Fatalf("live overlay must keep nonEgress %s scoped without env_file", service)
		}
	}
	valkeyBlock := composeServiceBlock(t, overlay, "valkey-cache")
	if strings.Contains(valkeyBlock, "command:") {
		t.Fatalf("live overlay must inherit valkey command from prod")
	}
	for _, service := range []string{"hololive-api", "hololive-alarm-worker"} {
		block := composeServiceBlock(t, overlay, service)
		if !strings.Contains(block, "IRIS_BASE_URL_ALLOWED_HOSTS: ${IRIS_BASE_URL_ALLOWED_HOSTS:-100.100.1.5}") {
			t.Fatalf("docker-compose.live-compat.yml missing IRIS_BASE_URL_ALLOWED_HOSTS default for %s", service)
		}
	}
}

func assertLiveCompatRenderedPortsAndModes(t *testing.T, cfg renderedCompose) {
	t.Helper()

	assertRenderedPort(t, cfg, "valkey-cache", "6379", "6379", "tcp")
	assertRenderedPortOnHost(t, cfg, "admin-dashboard", "127.0.0.1", "30190", "30190", "tcp")
	assertRenderedPort(t, cfg, "holo-postgres", "5433", "5432", "tcp")
	assertRenderedPort(t, cfg, "hololive-api", "30001", "30001", "tcp")
	assertRenderedPort(t, cfg, "hololive-api", "30001", "30001", "udp")

	if command := composeCommand(t, cfg, "valkey-cache"); !strings.Contains(command, "--unixsocketperm 777") {
		t.Fatalf("live overlay valkey command = %q, want --unixsocketperm 777", command)
	}

	for _, service := range []string{"holo-postgres", "hololive-db-migrate"} {
		if got := stringValue(composeService(t, cfg, service)["network_mode"]); got == "host" {
			t.Fatalf("%s network_mode = %q, want bridge networking", service, got)
		}
	}
}

func assertLiveCompatRenderedPostgres(t *testing.T, cfg renderedCompose) {
	t.Helper()

	migrationEnv := composeEnvironment(t, cfg, "hololive-db-migrate")
	if migrationEnv["PGHOST"] != "holo-postgres" || migrationEnv["PGPORT"] != "5432" {
		t.Fatalf("hololive-db-migrate PGHOST/PGPORT = %q/%q, want holo-postgres/5432", migrationEnv["PGHOST"], migrationEnv["PGPORT"])
	}

	postgresEnv := composeEnvironment(t, cfg, "holo-postgres")
	if postgresEnv["PGPORT"] != "5432" {
		t.Fatalf("holo-postgres PGPORT = %q, want 5432", postgresEnv["PGPORT"])
	}

	for _, service := range []string{"hololive-api", "hololive-alarm-worker", "youtube-producer"} {
		env := composeEnvironment(t, cfg, service)
		if env["POSTGRES_HOST"] != "holo-postgres" || env["POSTGRES_PORT"] != "5432" || env["POSTGRES_SSLMODE"] != "verify-full" {
			t.Fatalf("%s POSTGRES env = %q/%q/%q, want holo-postgres/5432/verify-full", service, env["POSTGRES_HOST"], env["POSTGRES_PORT"], env["POSTGRES_SSLMODE"])
		}
		if value, ok := env["POSTGRES_SSLMODE_ALLOW_INSECURE"]; ok {
			t.Fatalf("%s renders retired POSTGRES_SSLMODE_ALLOW_INSECURE=%q; verify-full replaced the downgrade path", service, value)
		}
		if env["POSTGRES_SSLROOTCERT"] != "/run/hololive-bot/certs/postgres-ca.pem" {
			t.Fatalf("%s POSTGRES_SSLROOTCERT = %q, want /run/hololive-bot/certs/postgres-ca.pem", service, env["POSTGRES_SSLROOTCERT"])
		}
		targets := strings.Join(composeVolumeTargets(t, cfg, service), "\n")
		for _, target := range []string{"/app/data", "/app/logs", "/app/runtime-config", "/run/hololive-bot/certs", "/var/run/valkey"} {
			if !strings.Contains(targets, target) {
				t.Fatalf("%s missing live-compat volume target %s in %q", service, target, targets)
			}
		}
	}
}

func assertLiveCompatRenderedSecrets(t *testing.T, cfg renderedCompose) {
	t.Helper()

	assertLiveCompatEgressSecrets(t, cfg)
	assertLiveCompatNonEgressSecrets(t, cfg)
	assertLiveCompatDashboardOrigin(t, cfg)
}

func assertLiveCompatEgressSecrets(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{"hololive-api", "hololive-alarm-worker"} {
		env := composeEnvironment(t, cfg, service)
		for _, key := range []string{"IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"} {
			if _, ok := env[key]; !ok {
				t.Fatalf("%s missing env_file-restored key %s", service, key)
			}
		}
	}
}

func assertLiveCompatNonEgressSecrets(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{"youtube-producer", "admin-dashboard"} {
		env := composeEnvironment(t, cfg, service)
		for _, key := range []string{"IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"} {
			if _, ok := env[key]; ok {
				t.Fatalf("nonEgress %s rendered with %s under live overlay", service, key)
			}
		}
		if service != "admin-dashboard" {
			for _, key := range []string{"ADMIN_PASS_BCRYPT", "ADMIN_PASS_HASH", "ADMIN_SECRET_KEY", "SESSION_SECRET"} {
				if _, ok := env[key]; ok {
					t.Fatalf("nonEgress %s rendered with dashboard-only secret %s under live overlay", service, key)
				}
			}
		}
	}
}

func assertLiveCompatDashboardOrigin(t *testing.T, cfg renderedCompose) {
	t.Helper()

	dashboardEnv := composeEnvironment(t, cfg, "admin-dashboard")
	if strings.Contains(dashboardEnv["ALLOWED_ORIGINS"], "100.100.1.3:30190") {
		t.Fatalf("admin-dashboard ALLOWED_ORIGINS = %q, want no default Tailnet origin", dashboardEnv["ALLOWED_ORIGINS"])
	}
	if !strings.Contains(dashboardEnv["ALLOWED_ORIGINS"], "https://admin.holo-oshi.com") {
		t.Fatalf("admin-dashboard ALLOWED_ORIGINS = %q, want explicit HTTPS admin origin", dashboardEnv["ALLOWED_ORIGINS"])
	}
}

func assertLiveCompatRenderedRuntimeConfig(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{"hololive-api", "hololive-alarm-worker"} {
		env := composeEnvironment(t, cfg, service)
		if env["IRIS_BASE_URL_FILE"] != "/app/runtime-config/iris_base_url" {
			t.Fatalf("%s IRIS_BASE_URL_FILE = %q, want /app/runtime-config/iris_base_url", service, env["IRIS_BASE_URL_FILE"])
		}
		if env["IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS"] != "true" {
			t.Fatalf("%s IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS = %q, want true", service, env["IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS"])
		}
		if env["IRIS_BASE_URL_ALLOWED_HOSTS"] != "100.100.1.5" {
			t.Fatalf("%s IRIS_BASE_URL_ALLOWED_HOSTS = %q, want 100.100.1.5", service, env["IRIS_BASE_URL_ALLOWED_HOSTS"])
		}
	}
}

func TestRepoAPDeployScriptsUseSplitRuntimeEnv(t *testing.T) {
	for _, file := range []string{
		"scripts/deploy/ap-deploy.sh",
		"scripts/deploy/ap-completion-check.sh",
		"scripts/deploy/ap-rollback.sh",
		"scripts/deploy/ap-iris-h3-trust-preflight.sh",
	} {
		content := readRepoFile(t, file)
		if strings.Contains(content, "/run/hololive-bot/env") {
			t.Fatalf("%s still references monolithic /run/hololive-bot/env", file)
		}
		if !strings.Contains(content, "/run/hololive-bot/ap-compose.env") {
			t.Fatalf("%s missing AP-safe compose env file contract", file)
		}
	}
}

func TestRepoAPDeployScriptsRequirePersistedQUICUDPBuffers(t *testing.T) {
	lib := readRepoFile(t, "scripts/deploy/lib/require-quic-udp-buffer.sh")
	for _, snippet := range []string{
		"net.core.rmem_max",
		"net.core.wmem_max",
		"/etc/sysctl.d/*.conf",
		"are not persisted",
	} {
		if !strings.Contains(lib, snippet) {
			t.Fatalf("require-quic-udp-buffer.sh missing runtime+persisted contract %q", snippet)
		}
	}

	for _, file := range []string{
		"scripts/deploy/ap-iris-h3-trust-preflight.sh",
		"scripts/deploy/ap-completion-check.sh",
	} {
		content := readRepoFile(t, file)
		if !strings.Contains(content, "require-quic-udp-buffer.sh") {
			t.Fatalf("%s must delegate QUIC UDP buffer checks to require-quic-udp-buffer.sh", file)
		}
		if strings.Contains(content, "sysctl -n net.core.rmem_max") {
			t.Fatalf("%s still uses runtime-only inline sysctl check", file)
		}
	}
}

// accepted-risk ledger는 verify-full 전환으로 종료되었다. 문서가 다시 생기거나
// compose 어디든 POSTGRES_SSLMODE_ALLOW_INSECURE가 재등장하면 회귀다.
func TestRepoPostgresSSLModeInsecureDowngradeIsRetired(t *testing.T) {
	root := repoRootFromConfigTest(t)
	ledgerPath := filepath.Join(root, "docs", "current", "security", "accepted-risk-ap-postgres-sslmode.md")
	if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
		t.Fatalf("accepted-risk-ap-postgres-sslmode.md still exists; the ledger exits with the verify-full transition (stat err=%v)", err)
	}

	composeDir := filepath.Join(root, "deploy", "compose")
	entries, err := os.ReadDir(composeDir)
	if err != nil {
		t.Fatalf("read compose dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		content := readRepoFile(t, filepath.Join("deploy", "compose", entry.Name()))
		if strings.Contains(content, "POSTGRES_SSLMODE_ALLOW_INSECURE") {
			t.Fatalf("deploy/compose/%s still references POSTGRES_SSLMODE_ALLOW_INSECURE; verify-full replaced the downgrade path", entry.Name())
		}
	}
}

// 모든 운영 스택 렌더에서 Postgres 클라이언트는 verify-full + 마운트된 CA 번들을
// 사용해야 한다(구 accepted-risk ledger의 exit criteria).
func TestRepoComposeAllStacksRenderVerifyFullPostgres(t *testing.T) {
	stacks := []struct {
		name     string
		files    []string
		services []string
	}{
		{
			name:     "base prod",
			files:    []string{"deploy/compose/docker-compose.prod.yml"},
			services: []string{"hololive-api", "hololive-alarm-worker", "youtube-producer"},
		},
		{
			name: "live-compat",
			files: []string{
				"deploy/compose/docker-compose.prod.yml",
				"deploy/compose/docker-compose.live-compat.yml",
			},
			services: []string{"hololive-api", "hololive-alarm-worker", "youtube-producer"},
		},
		{
			name: "main-ap live-compat",
			files: []string{
				"deploy/compose/docker-compose.prod.yml",
				"deploy/compose/docker-compose.live-compat.yml",
				"deploy/compose/docker-compose.main-ap.yml",
				"deploy/compose/docker-compose.main-ap.live-compat.yml",
			},
			services: []string{"youtube-producer-c"},
		},
	}

	for _, tt := range stacks {
		t.Run(tt.name, func(t *testing.T) {
			cfg := renderComposeConfig(t, tt.files...)
			for _, service := range tt.services {
				env := composeEnvironment(t, cfg, service)
				if env["POSTGRES_SSLMODE"] != "verify-full" {
					t.Fatalf("%s in %s POSTGRES_SSLMODE = %q, want verify-full", service, tt.name, env["POSTGRES_SSLMODE"])
				}
				if value, ok := env["POSTGRES_SSLMODE_ALLOW_INSECURE"]; ok {
					t.Fatalf("%s in %s renders retired POSTGRES_SSLMODE_ALLOW_INSECURE=%q", service, tt.name, value)
				}
				if env["POSTGRES_SSLROOTCERT"] != "/run/hololive-bot/certs/postgres-ca.pem" {
					t.Fatalf("%s in %s POSTGRES_SSLROOTCERT = %q, want /run/hololive-bot/certs/postgres-ca.pem", service, tt.name, env["POSTGRES_SSLROOTCERT"])
				}
			}
		})
	}
}

// holo-postgres는 server TLS를 켠 채로 기동해야 verify-full 클라이언트가 성립한다.
// server key는 클라이언트들이 통째로 마운트하는 certs/ 디렉토리 밖(postgres-tls/)에 둔다.
func TestRepoComposeHoloPostgresServesTLS(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []string
	}{
		{name: "base prod", files: []string{"deploy/compose/docker-compose.prod.yml"}},
		{name: "live-compat", files: []string{
			"deploy/compose/docker-compose.prod.yml",
			"deploy/compose/docker-compose.live-compat.yml",
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := renderComposeConfig(t, tt.files...)
			assertHoloPostgresTLSCommand(t, cfg, tt.name)
			assertHoloPostgresTLSMount(t, cfg, tt.name)
			assertDBMigrateVerifyFullTLS(t, cfg, tt.name)
		})
	}
}

func assertHoloPostgresTLSCommand(t *testing.T, cfg renderedCompose, stackName string) {
	t.Helper()

	command := composeCommand(t, cfg, "holo-postgres")
	for _, flag := range []string{
		"ssl=on",
		"ssl_cert_file=/run/hololive-bot/postgres-tls/server.crt",
		"ssl_key_file=/run/hololive-bot/postgres-tls/server.key",
	} {
		if !strings.Contains(command, flag) {
			t.Fatalf("holo-postgres command in %s missing %q: %q", stackName, flag, command)
		}
	}
}

func assertHoloPostgresTLSMount(t *testing.T, cfg renderedCompose, stackName string) {
	t.Helper()

	foundTLSMount := false
	for _, volume := range composeVolumes(t, cfg, "holo-postgres") {
		source := cleanVolumePath(volume.Source)
		target := cleanVolumePath(volume.Target)
		if source == "/run/hololive-bot/postgres-tls" && target == "/run/hololive-bot/postgres-tls" {
			if !volume.ReadOnly {
				t.Fatalf("holo-postgres postgres-tls mount must be read-only in %s", stackName)
			}
			foundTLSMount = true
		}
		if target == "/run/hololive-bot/certs" {
			t.Fatalf("holo-postgres must not mount the shared client certs directory in %s", stackName)
		}
	}
	if !foundTLSMount {
		t.Fatalf("holo-postgres missing /run/hololive-bot/postgres-tls read-only mount in %s", stackName)
	}
}

func assertDBMigrateVerifyFullTLS(t *testing.T, cfg renderedCompose, stackName string) {
	t.Helper()

	migrateEnv := composeEnvironment(t, cfg, "hololive-db-migrate")
	if migrateEnv["PGSSLMODE"] != "verify-full" {
		t.Fatalf("hololive-db-migrate PGSSLMODE = %q in %s, want verify-full", migrateEnv["PGSSLMODE"], stackName)
	}
	if migrateEnv["PGSSLROOTCERT"] != "/run/hololive-bot/certs/postgres-ca.pem" {
		t.Fatalf("hololive-db-migrate PGSSLROOTCERT = %q in %s, want /run/hololive-bot/certs/postgres-ca.pem", migrateEnv["PGSSLROOTCERT"], stackName)
	}
	migrateTargets := strings.Join(composeVolumeTargets(t, cfg, "hololive-db-migrate"), "\n")
	if !strings.Contains(migrateTargets, "/run/hololive-bot/certs/postgres-ca.pem") {
		t.Fatalf("hololive-db-migrate missing postgres-ca.pem mount in %s: %q", stackName, migrateTargets)
	}
}

func TestRepoComposeMainAPLiveCompatOverlayRestoresExtendedProducer(t *testing.T) {
	assertMainAPLiveCompatOverlayText(t)

	cfg := renderComposeConfig(t,
		"deploy/compose/docker-compose.prod.yml",
		"deploy/compose/docker-compose.live-compat.yml",
		"deploy/compose/docker-compose.main-ap.yml",
		"deploy/compose/docker-compose.main-ap.live-compat.yml",
	)

	assertMainAPLiveCompatRenderedEgressAllowedHosts(t, cfg)
	assertMainAPLiveCompatRenderedProducer(t, cfg)
}

func assertMainAPLiveCompatOverlayText(t *testing.T) {
	t.Helper()

	mainAP := readRepoFile(t, "deploy/compose/docker-compose.main-ap.yml")
	const producerEnvFile = "${HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE:-/run/hololive-bot/youtube-producer.env}"
	if block := composeServiceBlock(t, mainAP, "youtube-producer-c"); !strings.Contains(block, "env_file:") || !strings.Contains(block, producerEnvFile) {
		t.Fatalf("main-ap must give youtube-producer-c scoped env_file %q", producerEnvFile)
	}
}

func assertMainAPLiveCompatRenderedEgressAllowedHosts(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{"hololive-api", "hololive-alarm-worker"} {
		env := composeEnvironment(t, cfg, service)
		if env["IRIS_BASE_URL_ALLOWED_HOSTS"] != "100.100.1.5" {
			t.Fatalf("%s IRIS_BASE_URL_ALLOWED_HOSTS = %q, want 100.100.1.5", service, env["IRIS_BASE_URL_ALLOWED_HOSTS"])
		}
	}
}

func assertMainAPLiveCompatRenderedProducer(t *testing.T, cfg renderedCompose) {
	t.Helper()

	env := composeEnvironment(t, cfg, "youtube-producer-c")
	if env["POSTGRES_HOST"] != "holo-postgres" || env["POSTGRES_PORT"] != "5432" || env["POSTGRES_SSLMODE"] != "verify-full" {
		t.Fatalf("youtube-producer-c POSTGRES env = %q/%q/%q, want holo-postgres/5432/verify-full", env["POSTGRES_HOST"], env["POSTGRES_PORT"], env["POSTGRES_SSLMODE"])
	}
	if value, ok := env["POSTGRES_SSLMODE_ALLOW_INSECURE"]; ok {
		t.Fatalf("youtube-producer-c renders retired POSTGRES_SSLMODE_ALLOW_INSECURE=%q", value)
	}
	if env["POSTGRES_SSLROOTCERT"] != "/run/hololive-bot/certs/postgres-ca.pem" {
		t.Fatalf("youtube-producer-c POSTGRES_SSLROOTCERT = %q, want /run/hololive-bot/certs/postgres-ca.pem", env["POSTGRES_SSLROOTCERT"])
	}
	for _, key := range []string{"IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"} {
		if _, ok := env[key]; ok {
			t.Fatalf("youtube-producer-c rendered with %s under live overlay", key)
		}
	}
	for _, key := range []string{"API_SECRET_KEY", "HOLODEX_API_KEY", "HOLODEX_API_KEY_1"} {
		if _, ok := env[key]; !ok {
			t.Fatalf("youtube-producer-c missing scoped %s mapping", key)
		}
	}
	for _, key := range []string{"HOLODEX_API_KEY_2", "SCRAPER_PROXY_ENABLED", "YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", "YOUTUBE_ENABLE_QUOTA_BUILDING"} {
		if _, ok := env[key]; !ok {
			t.Fatalf("youtube-producer-c missing producer env_file key %s", key)
		}
	}

	targets := strings.Join(composeVolumeTargets(t, cfg, "youtube-producer-c"), "\n")
	for _, target := range []string{"/app/data", "/app/logs", "/app/runtime-config", "/run/hololive-bot/certs", "/var/run/valkey"} {
		if !strings.Contains(targets, target) {
			t.Fatalf("youtube-producer-c missing live-compat volume target %s in %q", target, targets)
		}
	}
}

func TestRepoComposeNoStackRendersWeakPostgresSSLMode(t *testing.T) {
	tests := []struct {
		name  string
		files []string
	}{
		{
			name:  "live-compat",
			files: []string{"deploy/compose/docker-compose.prod.yml", "deploy/compose/docker-compose.live-compat.yml"},
		},
		{
			name: "main-ap live-compat",
			files: []string{
				"deploy/compose/docker-compose.prod.yml",
				"deploy/compose/docker-compose.live-compat.yml",
				"deploy/compose/docker-compose.main-ap.yml",
				"deploy/compose/docker-compose.main-ap.live-compat.yml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := renderComposeConfig(t, tt.files...)
			for service := range cfg.Services {
				env := composeEnvironment(t, cfg, service)
				if isWeakPostgresSSLMode(env["POSTGRES_SSLMODE"]) {
					t.Fatalf("%s in %s renders weak POSTGRES_SSLMODE=%q; only verify-full is allowed", service, tt.name, env["POSTGRES_SSLMODE"])
				}
			}
		})
	}
}

func TestRepoCompose_StandaloneDispatcherServiceIsRemoved(t *testing.T) {
	content := readRepoFile(t, "deploy/compose/docker-compose.prod.yml")

	disallowed := []string{
		"\n  dispatcher-go:",
		"hololive-dispatcher-go",
		"legacy-dispatcher-go",
		"DISPATCHER_PORT",
		"30020",
	}
	for _, pattern := range disallowed {
		if strings.Contains(content, pattern) {
			t.Fatalf("docker-compose.prod.yml still contains standalone dispatcher pattern %q", pattern)
		}
	}
}

func TestRepoHololiveComposeUnitExecutesOnlyImmutableRootWrappers_03e6dca8(t *testing.T) {
	unit := readRepoFile(t, "scripts/systemd/hololive-compose.service")

	execDirectives := []string{
		"ExecStart=", "ExecStartPre=", "ExecStartPost=",
		"ExecReload=", "ExecStop=", "ExecStopPost=",
	}

	found := 0
	for line := range strings.SplitSeq(unit, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, directive := range execDirectives {
			if !strings.HasPrefix(trimmed, directive) {
				continue
			}
			found++
			value := strings.TrimPrefix(trimmed, directive)
			binary := systemdExecBinary(value)
			if !strings.HasPrefix(binary, "/usr/local/sbin/") {
				t.Fatalf("%s%s executes %q; a root unit must run only immutable root-owned /usr/local/sbin wrappers, never a kapu-writable repo/home path (privilege escalation 03e6dca8)", directive, value, binary)
			}
		}
	}

	if found == 0 {
		t.Fatal("hololive-compose.service declares no Exec* directives to verify (03e6dca8)")
	}
}

func systemdExecBinary(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "-@+!:")
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func isWeakPostgresSSLMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "disable", "allow", "prefer", "require", "verify-ca":
		return true
	default:
		return false
	}
}

type renderedCompose struct {
	Services map[string]map[string]any `yaml:"services"`
}

type renderedPort struct {
	HostIP    string
	Published string
	Target    string
	Protocol  string
}

func topLevelYAMLBlock(t *testing.T, content, headerPrefix string) string {
	t.Helper()

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, headerPrefix) {
			end := len(lines)
			for j := i + 1; j < len(lines); j++ {
				if lines[j] != "" && lines[j][0] != ' ' && lines[j][0] != '-' {
					end = j
					break
				}
			}
			return strings.Join(lines[i:end], "\n")
		}
	}

	t.Fatalf("top-level YAML block %s not found", headerPrefix)
	return ""
}

func composeServiceBlock(t *testing.T, content, service string) string {
	t.Helper()

	header := "  " + service + ":"
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if line == header {
			end := len(lines)
			for j := i + 1; j < len(lines); j++ {
				if regexp.MustCompile(`^ {2}[A-Za-z0-9_-]+:`).MatchString(lines[j]) {
					end = j
					break
				}
			}
			return strings.Join(lines[i:end], "\n")
		}
	}

	t.Fatalf("compose service %s not found", service)
	return ""
}

func renderComposeConfig(t *testing.T, files ...string) renderedCompose {
	t.Helper()

	return renderComposeConfigWithEnvFile(t, writeCentralComposeEnvFile(t), files...)
}

func renderComposeConfigWithEnvFile(t *testing.T, composeEnvFile string, files ...string) renderedCompose {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker CLI unavailable: %v", err)
	}

	ctx, cancel := dockerComposeConfigContext(t)
	defer cancel()
	cmd := dockerComposeConfigCommand(t, ctx, files)
	repoRoot := repoRootFromConfigTest(t)
	appEnvFile := writeCentralAppEnvFile(t)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"COMPOSE_ENV_FILE="+composeEnvFile,
		"HOLOLIVE_API_ENV_FILE="+appEnvFile,
		"HOLOLIVE_ALARM_WORKER_ENV_FILE="+appEnvFile,
		"HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE="+writeAPProducerEnvFile(t),
		"ADMIN_DASHBOARD_ENV_FILE="+writeAdminDashboardEnvFile(t),
		"DB_PASSWORD=dummy",
		"CACHE_PASSWORD=dummy",
		"IRIS_WEBHOOK_TOKEN=dummy",
		"IRIS_BOT_TOKEN=dummy",
		"ADMIN_PASS_BCRYPT=dummy",
		"SESSION_SECRET=dummy",
		"LIVE_LOGS_PATH=/srv/hololive-logs-dummy",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose config failed: %v\n%s", err, output)
	}

	var cfg renderedCompose
	decoder := yaml.NewDecoder(bytes.NewReader(output))
	decoder.KnownFields(false)
	if err := decoder.Decode(&cfg); err != nil {
		t.Fatalf("parse rendered compose failed: %v", err)
	}
	if len(cfg.Services) == 0 {
		t.Fatalf("rendered compose has no services")
	}

	return cfg
}

func dockerComposeConfigCommand(t *testing.T, ctx context.Context, files []string) *exec.Cmd {
	t.Helper()

	switch strings.Join(files, "\x00") {
	case "deploy/compose/docker-compose.prod.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "--profile", "main-ap", "-f", "deploy/compose/docker-compose.prod.yml", "config")
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.live-compat.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "--profile", "main-ap", "-f", "deploy/compose/docker-compose.prod.yml", "-f", "deploy/compose/docker-compose.live-compat.yml", "config")
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.live-compat.yml\x00deploy/compose/docker-compose.main-ap.yml\x00deploy/compose/docker-compose.main-ap.live-compat.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "--profile", "main-ap", "-f", "deploy/compose/docker-compose.prod.yml", "-f", "deploy/compose/docker-compose.live-compat.yml", "-f", "deploy/compose/docker-compose.main-ap.yml", "-f", "deploy/compose/docker-compose.main-ap.live-compat.yml", "config")
	default:
		t.Fatalf("unsupported compose file set: %v", files)
		return nil
	}
}

func dockerComposeConfigContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	if deadline, ok := t.Deadline(); ok {
		return context.WithDeadline(context.Background(), deadline)
	}
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func renderAPComposeConfig(t *testing.T, files ...string) renderedCompose {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker CLI unavailable: %v", err)
	}

	ctx, cancel := dockerComposeConfigContext(t)
	defer cancel()
	cmd := dockerAPComposeConfigCommand(t, ctx, files)
	repoRoot := repoRootFromConfigTest(t)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"COMPOSE_ENV_FILE="+writeAPComposeEnvFile(t),
		"HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE="+writeAPProducerEnvFile(t),
		"ADMIN_DASHBOARD_ENV_FILE="+writeAdminDashboardEnvFile(t),
		"DB_PASSWORD=dummy",
		"CACHE_PASSWORD=dummy",
		"ADMIN_PASS_BCRYPT=dummy",
		"SESSION_SECRET=dummy",
		"SEOUL_CACHE_HOST=stub",
		"SEOUL_POSTGRES_HOST=stub",
		"SEOUL_CLIPROXY_BASE_URL=https://cliproxy.invalid",
		"SEOUL_METRICS_BIND_IP=100.100.1.5",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose AP config failed: %v\n%s", err, output)
	}

	var cfg renderedCompose
	if err := yaml.Unmarshal(output, &cfg); err != nil {
		t.Fatalf("decode docker compose AP config: %v\n%s", err, output)
	}
	return cfg
}

func dockerAPComposeConfigCommand(t *testing.T, ctx context.Context, files []string) *exec.Cmd {
	t.Helper()

	switch strings.Join(files, "\x00") {
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.osaka.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "-f", "deploy/compose/docker-compose.prod.yml", "-f", "deploy/compose/docker-compose.osaka.yml", "config")
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.osaka2.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "-f", "deploy/compose/docker-compose.prod.yml", "-f", "deploy/compose/docker-compose.osaka2.yml", "config")
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.seoul.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "-f", "deploy/compose/docker-compose.prod.yml", "-f", "deploy/compose/docker-compose.seoul.yml", "config")
	default:
		t.Fatalf("unsupported AP compose file set: %v", files)
		return nil
	}
}

func writeCentralComposeEnvFile(t *testing.T) string {
	t.Helper()

	return writeTempEnvFile(t, "central-compose-*.env", []string{
		"ADMIN_PASS_BCRYPT=dummy",
		"CACHE_PASSWORD=dummy",
		"DB_PASSWORD=dummy",
		"IRIS_WEBHOOK_TOKEN=dummy",
		"IRIS_BOT_TOKEN=dummy",
		"SESSION_SECRET=dummy",
	})
}

// /run/hololive-bot/admin-dashboard.env는 0600 root 렌더 파일이라 kapu로 도는 테스트는
// 기본 경로를 열 수 없다(required:false는 부재만 허용). 셸 테스트와 동일하게 스텁으로 대체한다.
func writeAdminDashboardEnvFile(t *testing.T) string {
	t.Helper()

	return writeTempEnvFile(t, "admin-dashboard-*.env", []string{
		"ADMIN_PASS_HASH=dummy",
		"SESSION_SECRET=dummy",
		"VALKEY_URL=:dummy@valkey-cache:6379",
		"HOLO_BOT_API_KEY=dummy",
	})
}

func writeCentralAppEnvFile(t *testing.T) string {
	t.Helper()

	return writeTempEnvFile(t, "central-app-*.env", []string{
		"API_SECRET_KEY=dummy",
	})
}

func writeAPComposeEnvFile(t *testing.T) string {
	t.Helper()

	return writeTempEnvFile(t, "ap-compose-*.env", []string{
		"ADMIN_PASS_BCRYPT=dummy",
		"CACHE_PASSWORD=dummy",
		"DB_PASSWORD=dummy",
		"SESSION_SECRET=dummy",
	})
}

func renderableAPComposeFile(t *testing.T, relativePath string) string {
	t.Helper()

	return writeRenderableAPComposeFile(t, relativePath, readRepoFile(t, relativePath))
}

func writeRenderableAPComposeFile(t *testing.T, sourceName, content string) string {
	t.Helper()

	if strings.Contains(content, "/run/hololive-bot/env") || strings.Contains(content, "COMPOSE_ENV_FILE") {
		t.Fatalf("%s must not reference monolithic hololive env file", sourceName)
	}
	const producerEnvFile = "${HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE:-/run/hololive-bot/youtube-producer.env}"
	if !strings.Contains(content, producerEnvFile) {
		t.Fatalf("%s missing AP youtube-producer env_file path %s", sourceName, producerEnvFile)
	}

	return sourceName
}

func writeAPProducerEnvFile(t *testing.T) string {
	t.Helper()

	return writeTempEnvFile(t, "youtube-producer-*.env", []string{
		"API_SECRET_KEY=dummy",
		"HOLODEX_API_KEY=dummy",
		"HOLODEX_API_KEY_1=dummy",
		"HOLODEX_API_KEY_2=dummy",
		"HOLODEX_API_KEY_3=dummy",
		"HOLODEX_API_KEY_4=dummy",
		"HOLODEX_API_KEY_5=dummy",
		"SCRAPER_PROXY_ENABLED=false",
		"SCRAPER_PROXY_URL=http://proxy.invalid",
		"YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT=2026-04-10T01:11:12Z",
		"YOUTUBE_ENABLE_QUOTA_BUILDING=true",
	})
}

func writeTempEnvFile(t *testing.T, pattern string, lines []string) string {
	t.Helper()

	tempFile, err := os.CreateTemp(t.TempDir(), pattern)
	if err != nil {
		t.Fatalf("create temp env file failed: %v", err)
	}
	tempPath := tempFile.Name()

	content := strings.Join(lines, "\n") + "\n"

	if _, err := tempFile.WriteString(content); err != nil {
		if closeErr := tempFile.Close(); closeErr != nil {
			err = fmt.Errorf("%w; close temp env file: %w", err, closeErr)
		}
		t.Fatalf("write temp env file failed: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("close temp env file failed: %v", err)
	}

	return tempPath
}

func assertAPComposeCertMountsAreMinimized(t *testing.T, cfg renderedCompose, composeFile string) {
	t.Helper()

	serviceNames := apComposeServiceNames(t, cfg, composeFile)
	for _, service := range serviceNames {
		assertAPComposeServiceCertMounts(t, cfg, composeFile, service)
		assertAPComposeServiceEnvIsolation(t, cfg, composeFile, service)
	}
}

func assertAPComposeServiceCertMounts(t *testing.T, cfg renderedCompose, composeFile, service string) {
	t.Helper()

	hasIrisCA := false
	hasPostgresCA := false
	for _, volume := range composeVolumes(t, cfg, service) {
		source := cleanVolumePath(volume.Source)
		target := cleanVolumePath(volume.Target)
		if source == "/run/hololive-bot/certs" && target == "/run/hololive-bot/certs" {
			t.Fatalf("%s %s mounts broad cert directory: source=%q target=%q", composeFile, service, volume.Source, volume.Target)
		}
		isH3ServerKey := source == "/run/hololive-bot/certs/hololive-h3.key" && target == "/run/hololive-bot/certs/hololive-h3.key"
		if (strings.HasSuffix(volume.Source, ".key") || strings.HasSuffix(volume.Target, ".key")) && !isH3ServerKey {
			t.Fatalf("%s %s mounts private key file: source=%q target=%q", composeFile, service, volume.Source, volume.Target)
		}
		if target == "/run/hololive-bot/certs/iris-ca.pem" {
			hasIrisCA = true
		}
		if target == "/run/hololive-bot/certs/postgres-ca.pem" {
			hasPostgresCA = true
		}
	}
	if !hasIrisCA {
		t.Fatalf("%s %s missing iris-ca.pem mount - producer config load fetches the Iris webhook worker profile over H3 at startup", composeFile, service)
	}
	if !hasPostgresCA {
		t.Fatalf("%s %s missing postgres-ca.pem mount - verify-full needs the CA bundle over the Tailscale Postgres path", composeFile, service)
	}
}

func assertAPComposeServiceEnvIsolation(t *testing.T, cfg renderedCompose, composeFile, service string) {
	t.Helper()

	env := composeEnvironment(t, cfg, service)
	if value, ok := env["POSTGRES_SSLMODE_ALLOW_INSECURE"]; ok {
		t.Fatalf("%s %s renders retired POSTGRES_SSLMODE_ALLOW_INSECURE=%q", composeFile, service, value)
	}
	if env["POSTGRES_SSLROOTCERT"] != "/run/hololive-bot/certs/postgres-ca.pem" {
		t.Fatalf("%s %s POSTGRES_SSLROOTCERT = %q, want /run/hololive-bot/certs/postgres-ca.pem", composeFile, service, env["POSTGRES_SSLROOTCERT"])
	}
	for _, key := range []string{"IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"} {
		if _, ok := env[key]; ok {
			t.Fatalf("%s %s rendered with Iris egress token %s", composeFile, service, key)
		}
	}
}

func assertAPComposeDoesNotRequireCentralEgressEnvFiles(t *testing.T, cfg renderedCompose, composeFile string) {
	t.Helper()

	for _, service := range []string{"hololive-api", "hololive-alarm-worker"} {
		if _, ok := cfg.Services[service]; !ok {
			continue
		}
		if envFile, ok := composeService(t, cfg, service)["env_file"]; ok {
			t.Fatalf("%s %s must not require central egress env_file on AP host: %v", composeFile, service, envFile)
		}
	}
}

func apComposeServiceNames(t *testing.T, cfg renderedCompose, composeFile string) []string {
	t.Helper()

	serviceNames := make([]string, 0, len(cfg.Services))
	for service := range cfg.Services {
		if strings.HasPrefix(service, "youtube-producer") {
			serviceNames = append(serviceNames, service)
		}
	}
	if len(serviceNames) == 0 {
		t.Fatalf("%s rendered no AP youtube-producer services", composeFile)
	}
	return serviceNames
}

func cleanVolumePath(value string) string {
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func composeService(t *testing.T, cfg renderedCompose, service string) map[string]any {
	t.Helper()

	value, ok := cfg.Services[service]
	if !ok {
		t.Fatalf("rendered compose missing service %s", service)
	}
	return value
}

func composeEnvironment(t *testing.T, cfg renderedCompose, service string) map[string]string {
	t.Helper()

	raw, ok := composeService(t, cfg, service)["environment"]
	if !ok {
		return map[string]string{}
	}

	result := make(map[string]string)
	switch env := raw.(type) {
	case map[string]any:
		for key, value := range env {
			result[key] = stringValue(value)
		}
	default:
		t.Fatalf("%s environment has unexpected type %T", service, raw)
	}
	return result
}

func composePorts(t *testing.T, serviceName string, service map[string]any) []renderedPort {
	t.Helper()

	raw, ok := service["ports"]
	if !ok {
		return nil
	}

	values, ok := raw.([]any)
	if !ok {
		t.Fatalf("%s ports has unexpected type %T", serviceName, raw)
	}

	ports := make([]renderedPort, 0, len(values))
	for _, value := range values {
		portMap, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s port has unexpected type %T", serviceName, value)
		}
		ports = append(ports, renderedPort{
			HostIP:    stringValue(portMap["host_ip"]),
			Published: stringValue(portMap["published"]),
			Target:    stringValue(portMap["target"]),
			Protocol:  stringValue(portMap["protocol"]),
		})
	}
	return ports
}

func assertRenderedPort(t *testing.T, cfg renderedCompose, service, published, target, protocol string) {
	t.Helper()

	assertRenderedPortOnHost(t, cfg, service, "100.100.1.3", published, target, protocol)
}

func assertRenderedPortOnHost(t *testing.T, cfg renderedCompose, service, hostIP, published, target, protocol string) {
	t.Helper()

	for _, port := range composePorts(t, service, composeService(t, cfg, service)) {
		if port.HostIP == hostIP && port.Published == published && port.Target == target && port.Protocol == protocol {
			return
		}
	}
	t.Fatalf("%s missing rendered port %s:%s:%s/%s", service, hostIP, published, target, protocol)
}

func composeVolumeTargets(t *testing.T, cfg renderedCompose, service string) []string {
	t.Helper()

	volumes := composeVolumes(t, cfg, service)
	targets := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		targets = append(targets, volume.Target)
	}
	return targets
}

type renderedVolume struct {
	Source   string
	Target   string
	ReadOnly bool
}

func composeVolumes(t *testing.T, cfg renderedCompose, service string) []renderedVolume {
	t.Helper()

	raw, ok := composeService(t, cfg, service)["volumes"]
	if !ok {
		return nil
	}

	values, ok := raw.([]any)
	if !ok {
		t.Fatalf("%s volumes has unexpected type %T", service, raw)
	}

	volumes := make([]renderedVolume, 0, len(values))
	for _, value := range values {
		volumeMap, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s volume has unexpected type %T", service, value)
		}
		volumes = append(volumes, renderedVolume{
			Source:   stringValue(volumeMap["source"]),
			Target:   stringValue(volumeMap["target"]),
			ReadOnly: volumeMap["read_only"] == true,
		})
	}
	return volumes
}

func composeCommand(t *testing.T, cfg renderedCompose, service string) string {
	t.Helper()

	raw, ok := composeService(t, cfg, service)["command"]
	if !ok {
		return ""
	}

	switch command := raw.(type) {
	case []any:
		parts := make([]string, 0, len(command))
		for _, part := range command {
			parts = append(parts, stringValue(part))
		}
		return strings.Join(parts, " ")
	default:
		return stringValue(raw)
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}
