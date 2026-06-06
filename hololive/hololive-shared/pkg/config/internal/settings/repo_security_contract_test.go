package settings

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

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

func TestRepoComposeProdHardenedDefaults(t *testing.T) {
	content := readRepoFile(t, "deploy/compose/docker-compose.prod.yml")

	disallowed := []string{
		"100.100.1.3",
		"${VALKEY_PORT_BIND_IP:-100.100.1.3}:6379:6379",
		"${ADMIN_DASHBOARD_PORT_BIND_IP:-100.100.1.3}:30190:30190",
		"${HOLOLIVE_BOT_PORT_BIND_IP:-100.100.1.3}:30001:30001",
		"network_mode: host",
		"--unixsocketperm 777",
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

	if got := strings.Count(content, "POSTGRES_HOST: holo-postgres"); got != 1 {
		t.Fatalf("docker-compose.prod.yml POSTGRES_HOST holo-postgres anchor count = %d, want 1", got)
	}
	if got := strings.Count(content, "POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-verify-full}"); got != 1 {
		t.Fatalf("docker-compose.prod.yml POSTGRES_SSLMODE verify-full default count = %d, want 1", got)
	}
	if got := strings.Count(content, "*postgres-env"); got != 5 {
		t.Fatalf("docker-compose.prod.yml postgres env anchor usage count = %d, want 5", got)
	}

	required := []string{
		"holo-postgres:",
		"    networks:",
		"x-postgres-env: &postgres-env",
		"  POSTGRES_PORT: \"5432\"",
		"  POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-verify-full}",
		"  IRIS_BASE_URL_FILE: ${IRIS_BASE_URL_FILE:-}",
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

	egressOwners := []string{"hololive-bot", "hololive-alarm-worker"}
	for _, service := range egressOwners {
		block := composeServiceBlock(t, content, service)
		wantEnvFile := map[string]string{
			"hololive-bot":          "${HOLOLIVE_BOT_ENV_FILE:-/run/hololive-bot/bot.env}",
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

	nonEgress := []string{"hololive-admin-api", "youtube-producer", "llm-scheduler", "admin-dashboard"}
	for _, service := range nonEgress {
		block := composeServiceBlock(t, content, service)
		if strings.Contains(block, "env_file:") {
			t.Fatalf("%s must not define env_file in hardened docker-compose.prod.yml", service)
		}
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

func TestRepoComposeProdRenderedIsolation(t *testing.T) {
	cfg := renderComposeConfig(t, "deploy/compose/docker-compose.prod.yml")

	for _, service := range []string{"holo-postgres", "hololive-db-migrate"} {
		if got := stringValue(composeService(t, cfg, service)["network_mode"]); got == "host" {
			t.Fatalf("%s rendered with network_mode=host", service)
		}
	}

	for _, service := range []string{"hololive-bot", "hololive-admin-api", "hololive-alarm-worker", "youtube-producer", "llm-scheduler"} {
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

	for _, service := range []string{"hololive-admin-api", "youtube-producer", "llm-scheduler", "admin-dashboard"} {
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

	for _, service := range []string{"hololive-bot", "hololive-alarm-worker"} {
		env := composeEnvironment(t, cfg, service)
		for _, key := range []string{
			"KAKAO_ROOMS",
			"KAKAO_ACL_ENABLED",
			"KAKAO_ACL_MODE",
			"API_SECRET_KEY",
			"HOLODEX_API_KEY",
			"HOLODEX_API_KEY_1",
			"YOUTUBE_API_KEY",
		} {
			if _, ok := env[key]; !ok {
				t.Fatalf("%s missing egress runtime key %s", service, key)
			}
		}
	}

	for _, service := range []string{"youtube-producer", "llm-scheduler"} {
		env := composeEnvironment(t, cfg, service)
		if _, ok := env["API_SECRET_KEY"]; !ok {
			t.Fatalf("%s missing scoped API_SECRET_KEY mapping", service)
		}
	}

	producerEnv := composeEnvironment(t, cfg, "youtube-producer")
	for _, key := range []string{"YOUTUBE_API_KEY", "HOLODEX_API_KEY", "HOLODEX_API_KEY_1"} {
		if _, ok := producerEnv[key]; !ok {
			t.Fatalf("youtube-producer missing scoped %s mapping", key)
		}
	}
	if producerEnv["HOLOLIVE_HTTP_TRANSPORTS"] != "h3" {
		t.Fatalf("youtube-producer HOLOLIVE_HTTP_TRANSPORTS = %q, want h3", producerEnv["HOLOLIVE_HTTP_TRANSPORTS"])
	}

	for _, service := range []string{"hololive-bot", "hololive-alarm-worker"} {
		env := composeEnvironment(t, cfg, service)
		if env["IRIS_BASE_URL_FILE"] != "" {
			t.Fatalf("%s IRIS_BASE_URL_FILE = %q, want empty default", service, env["IRIS_BASE_URL_FILE"])
		}
	}

	for _, service := range []string{"hololive-admin-api", "youtube-producer", "llm-scheduler", "admin-dashboard"} {
		for _, target := range composeVolumeTargets(t, cfg, service) {
			if target == "/app/runtime-config" {
				t.Fatalf("%s still mounts runtime-config", service)
			}
		}
	}

	h3KeyConsumers := map[string]bool{
		"hololive-bot":          true,
		"hololive-admin-api":    true,
		"hololive-alarm-worker": true,
		"youtube-producer":      true,
		"llm-scheduler":         true,
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
	for _, service := range []string{"hololive-bot", "hololive-alarm-worker"} {
		block := composeServiceBlock(t, overlay, service)
		wantEnvFile := map[string]string{
			"hololive-bot":          "${HOLOLIVE_BOT_ENV_FILE:-/run/hololive-bot/bot.env}",
			"hololive-alarm-worker": "${HOLOLIVE_ALARM_WORKER_ENV_FILE:-/run/hololive-bot/alarm-worker.env}",
		}[service]
		if !strings.Contains(block, "env_file:") || !strings.Contains(block, wantEnvFile) {
			t.Fatalf("live overlay must keep per-service env_file %q for %s", wantEnvFile, service)
		}
		if strings.Contains(block, "/run/hololive-bot/env") || strings.Contains(block, "COMPOSE_ENV_FILE") {
			t.Fatalf("live overlay must not restore monolithic env_file for %s", service)
		}
	}
	for _, service := range []string{"hololive-admin-api", "youtube-producer", "llm-scheduler", "admin-dashboard"} {
		block := composeServiceBlock(t, overlay, service)
		if strings.Contains(block, "env_file:") {
			t.Fatalf("live overlay must keep nonEgress %s scoped without env_file", service)
		}
	}
	for _, service := range []string{"hololive-bot", "hololive-alarm-worker"} {
		block := composeServiceBlock(t, overlay, service)
		if !strings.Contains(block, "IRIS_BASE_URL_ALLOWED_HOSTS: ${IRIS_BASE_URL_ALLOWED_HOSTS:-100.100.1.5}") {
			t.Fatalf("docker-compose.live-compat.yml missing IRIS_BASE_URL_ALLOWED_HOSTS default for %s", service)
		}
	}

	cfg := renderComposeConfig(t, "deploy/compose/docker-compose.prod.yml", "deploy/compose/docker-compose.live-compat.yml")

	assertRenderedPort(t, cfg, "valkey-cache", "100.100.1.3", "6379", "6379", "tcp")
	assertRenderedPort(t, cfg, "admin-dashboard", "100.100.1.3", "30190", "30190", "tcp")
	assertRenderedPort(t, cfg, "hololive-bot", "100.100.1.3", "30001", "30001", "tcp")
	assertRenderedPort(t, cfg, "hololive-bot", "100.100.1.3", "30001", "30001", "udp")

	if command := composeCommand(t, cfg, "valkey-cache"); !strings.Contains(command, "--unixsocketperm 777") {
		t.Fatalf("live overlay valkey command = %q, want --unixsocketperm 777", command)
	}

	for _, service := range []string{"holo-postgres", "hololive-db-migrate"} {
		if got := stringValue(composeService(t, cfg, service)["network_mode"]); got != "host" {
			t.Fatalf("%s network_mode = %q, want host", service, got)
		}
	}

	migrationEnv := composeEnvironment(t, cfg, "hololive-db-migrate")
	if migrationEnv["PGHOST"] != "127.0.0.1" || migrationEnv["PGPORT"] != "5433" {
		t.Fatalf("hololive-db-migrate PGHOST/PGPORT = %q/%q, want 127.0.0.1/5433", migrationEnv["PGHOST"], migrationEnv["PGPORT"])
	}

	postgresEnv := composeEnvironment(t, cfg, "holo-postgres")
	if postgresEnv["PGPORT"] != "5433" {
		t.Fatalf("holo-postgres PGPORT = %q, want 5433", postgresEnv["PGPORT"])
	}

	for _, service := range []string{"hololive-bot", "hololive-admin-api", "hololive-alarm-worker", "youtube-producer", "llm-scheduler"} {
		env := composeEnvironment(t, cfg, service)
		if env["POSTGRES_HOST"] != "host.docker.internal" || env["POSTGRES_PORT"] != "5433" || env["POSTGRES_SSLMODE"] != "require" {
			t.Fatalf("%s POSTGRES env = %q/%q/%q, want host.docker.internal/5433/require", service, env["POSTGRES_HOST"], env["POSTGRES_PORT"], env["POSTGRES_SSLMODE"])
		}
		if env["POSTGRES_SSLMODE_ALLOW_INSECURE"] != "true" {
			t.Fatalf("%s POSTGRES_SSLMODE_ALLOW_INSECURE = %q, want true", service, env["POSTGRES_SSLMODE_ALLOW_INSECURE"])
		}
		targets := strings.Join(composeVolumeTargets(t, cfg, service), "\n")
		for _, target := range []string{"/app/data", "/app/logs", "/app/runtime-config", "/run/hololive-bot/certs", "/var/run/valkey"} {
			if !strings.Contains(targets, target) {
				t.Fatalf("%s missing live-compat volume target %s in %q", service, target, targets)
			}
		}
	}

	for _, service := range []string{"hololive-bot", "hololive-alarm-worker"} {
		env := composeEnvironment(t, cfg, service)
		for _, key := range []string{"IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"} {
			if _, ok := env[key]; !ok {
				t.Fatalf("%s missing env_file-restored key %s", service, key)
			}
		}
	}

	for _, service := range []string{"hololive-admin-api", "youtube-producer", "llm-scheduler", "admin-dashboard"} {
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

	dashboardEnv := composeEnvironment(t, cfg, "admin-dashboard")
	if !strings.Contains(dashboardEnv["ALLOWED_ORIGINS"], "http://100.100.1.3:30190") {
		t.Fatalf("admin-dashboard ALLOWED_ORIGINS = %q, want Tailnet origin restored", dashboardEnv["ALLOWED_ORIGINS"])
	}

	for _, service := range []string{"hololive-bot", "hololive-alarm-worker"} {
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

func TestRepoAPPostgresSSLModeDowngradeHasAcceptedRiskLedger(t *testing.T) {
	ledger := readRepoFile(t, "docs/current/security/accepted-risk-ap-postgres-sslmode.md")
	for _, snippet := range []string{
		"POSTGRES_SSLMODE_ALLOW_INSECURE=true",
		"Expiry:",
		"youtube-producer",
		"verify-full",
		"docker-compose.live-compat.yml",
	} {
		if !strings.Contains(ledger, snippet) {
			t.Fatalf("accepted-risk-ap-postgres-sslmode.md missing required ledger field %q", snippet)
		}
	}
}

// ledger의 적용 범위 서술은 실제 compose overlay 렌더 결과와 일치해야 한다:
// 기본 스택(prod 단독)은 중앙 서비스에 insecure 플래그를 렌더하지 않고,
// AP live overlay는 youtube-producer-c에만 렌더한다. central live-compat은
// opt-in 예외로 ledger 본문에 명시적으로 기록되어야 한다(위 테스트의
// docker-compose.live-compat.yml snippet 계약).
func TestRepoAPPostgresSSLModeLedgerScopeMatchesComposeRendering(t *testing.T) {
	baseCfg := renderComposeConfig(t, "deploy/compose/docker-compose.prod.yml")
	for _, service := range []string{"hololive-bot", "hololive-admin-api", "hololive-alarm-worker", "youtube-producer", "llm-scheduler"} {
		env := composeEnvironment(t, baseCfg, service)
		if value, ok := env["POSTGRES_SSLMODE_ALLOW_INSECURE"]; ok && value == "true" {
			t.Fatalf("base prod stack renders %s with POSTGRES_SSLMODE_ALLOW_INSECURE=true; insecure downgrade must stay opt-in via live-compat overlays", service)
		}
	}

	apCfg := renderComposeConfig(t,
		"deploy/compose/docker-compose.prod.yml",
		"deploy/compose/docker-compose.live-compat.yml",
		"deploy/compose/docker-compose.main-ap.yml",
		"deploy/compose/docker-compose.main-ap.live-compat.yml",
	)
	producerEnv := composeEnvironment(t, apCfg, "youtube-producer-c")
	if producerEnv["POSTGRES_SSLMODE_ALLOW_INSECURE"] != "true" {
		t.Fatalf("AP live overlay must keep the accepted-risk insecure flag for youtube-producer-c, got %q", producerEnv["POSTGRES_SSLMODE_ALLOW_INSECURE"])
	}
}

func TestRepoComposeMainAPLiveCompatOverlayRestoresExtendedProducer(t *testing.T) {
	overlay := readRepoFile(t, "deploy/compose/docker-compose.main-ap.live-compat.yml")
	for _, service := range []string{"hololive-bot", "hololive-alarm-worker"} {
		block := composeServiceBlock(t, overlay, service)
		if !strings.Contains(block, "IRIS_BASE_URL_ALLOWED_HOSTS: ${IRIS_BASE_URL_ALLOWED_HOSTS:-100.100.1.5}") {
			t.Fatalf("docker-compose.main-ap.live-compat.yml missing IRIS_BASE_URL_ALLOWED_HOSTS default for %s", service)
		}
	}
	if block := composeServiceBlock(t, overlay, "youtube-producer-c"); strings.Contains(block, "env_file:") {
		t.Fatal("main-ap live overlay must keep youtube-producer-c scoped without env_file")
	}

	cfg := renderComposeConfig(t,
		"deploy/compose/docker-compose.prod.yml",
		"deploy/compose/docker-compose.live-compat.yml",
		"deploy/compose/docker-compose.main-ap.yml",
		"deploy/compose/docker-compose.main-ap.live-compat.yml",
	)

	for _, service := range []string{"hololive-bot", "hololive-alarm-worker"} {
		env := composeEnvironment(t, cfg, service)
		if env["IRIS_BASE_URL_ALLOWED_HOSTS"] != "100.100.1.5" {
			t.Fatalf("%s IRIS_BASE_URL_ALLOWED_HOSTS = %q, want 100.100.1.5", service, env["IRIS_BASE_URL_ALLOWED_HOSTS"])
		}
	}

	env := composeEnvironment(t, cfg, "youtube-producer-c")
	if env["POSTGRES_HOST"] != "host.docker.internal" || env["POSTGRES_PORT"] != "5433" || env["POSTGRES_SSLMODE"] != "require" {
		t.Fatalf("youtube-producer-c POSTGRES env = %q/%q/%q, want host.docker.internal/5433/require", env["POSTGRES_HOST"], env["POSTGRES_PORT"], env["POSTGRES_SSLMODE"])
	}
	if env["POSTGRES_SSLMODE_ALLOW_INSECURE"] != "true" {
		t.Fatalf("youtube-producer-c POSTGRES_SSLMODE_ALLOW_INSECURE = %q, want true", env["POSTGRES_SSLMODE_ALLOW_INSECURE"])
	}
	for _, key := range []string{"IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"} {
		if _, ok := env[key]; ok {
			t.Fatalf("youtube-producer-c rendered with %s under live overlay", key)
		}
	}
	for _, key := range []string{"API_SECRET_KEY", "YOUTUBE_API_KEY", "HOLODEX_API_KEY", "HOLODEX_API_KEY_1"} {
		if _, ok := env[key]; !ok {
			t.Fatalf("youtube-producer-c missing scoped %s mapping", key)
		}
	}

	targets := strings.Join(composeVolumeTargets(t, cfg, "youtube-producer-c"), "\n")
	for _, target := range []string{"/app/data", "/app/logs", "/app/runtime-config", "/run/hololive-bot/certs", "/var/run/valkey"} {
		if !strings.Contains(targets, target) {
			t.Fatalf("youtube-producer-c missing live-compat volume target %s in %q", target, targets)
		}
	}
}

func TestRepoComposeLiveCompatWeakPostgresSSLModesCarryEscapeHatch(t *testing.T) {
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
				if !isWeakPostgresSSLMode(env["POSTGRES_SSLMODE"]) {
					continue
				}
				if env["POSTGRES_SSLMODE_ALLOW_INSECURE"] != "true" {
					t.Fatalf("%s in %s has POSTGRES_SSLMODE=%q without POSTGRES_SSLMODE_ALLOW_INSECURE=true", service, tt.name, env["POSTGRES_SSLMODE"])
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
				if regexp.MustCompile(`^  [A-Za-z0-9_-]+:`).MatchString(lines[j]) {
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

	args := []string{"compose", "--profile", "oracle", "--profile", "main-ap"}
	for _, file := range files {
		args = append(args, "-f", file)
	}
	args = append(args, "config")

	cmd := exec.Command("docker", args...)
	repoRoot := repoRootFromConfigTest(t)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"COMPOSE_ENV_FILE="+composeEnvFile,
		"HOLOLIVE_BOT_ENV_FILE="+composeEnvFile,
		"HOLOLIVE_ALARM_WORKER_ENV_FILE="+composeEnvFile,
		"HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE="+writeAPProducerEnvFile(t),
		"DB_PASSWORD=dummy",
		"CACHE_PASSWORD=dummy",
		"IRIS_WEBHOOK_TOKEN=dummy",
		"IRIS_BOT_TOKEN=dummy",
		"ADMIN_PASS_BCRYPT=dummy",
		"SESSION_SECRET=dummy",
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

func renderAPComposeConfig(t *testing.T, files ...string) renderedCompose {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker CLI unavailable: %v", err)
	}

	args := []string{"compose", "--profile", "oracle"}
	for _, file := range files {
		args = append(args, "-f", file)
	}
	args = append(args, "config")

	cmd := exec.Command("docker", args...)
	repoRoot := repoRootFromConfigTest(t)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"COMPOSE_ENV_FILE="+writeAPComposeEnvFile(t),
		"HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE="+writeAPProducerEnvFile(t),
		"DB_PASSWORD=dummy",
		"CACHE_PASSWORD=dummy",
		"ADMIN_PASS_BCRYPT=dummy",
		"SESSION_SECRET=dummy",
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
		"YOUTUBE_API_KEY=dummy",
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
		_ = tempFile.Close()
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
		hasIrisCA := false
		for _, volume := range composeVolumes(t, cfg, service) {
			source := cleanVolumePath(volume.Source)
			target := cleanVolumePath(volume.Target)
			if source == "/run/hololive-bot/certs" && target == "/run/hololive-bot/certs" {
				t.Fatalf("%s %s mounts broad cert directory: source=%q target=%q", composeFile, service, volume.Source, volume.Target)
			}
			// h3-only AP producer는 H3(QUIC) 서버라 서버 키가 필요하다 — 해당 키 단일 파일만 허용.
			isH3ServerKey := source == "/run/hololive-bot/certs/hololive-h3.key" && target == "/run/hololive-bot/certs/hololive-h3.key"
			if (strings.HasSuffix(volume.Source, ".key") || strings.HasSuffix(volume.Target, ".key")) && !isH3ServerKey {
				t.Fatalf("%s %s mounts private key file: source=%q target=%q", composeFile, service, volume.Source, volume.Target)
			}
			if target == "/run/hololive-bot/certs/iris-ca.pem" {
				hasIrisCA = true
			}
		}
		if !hasIrisCA {
			t.Fatalf("%s %s missing iris-ca.pem mount — producer config load fetches the Iris webhook worker profile over H3 at startup", composeFile, service)
		}

		env := composeEnvironment(t, cfg, service)
		if env["POSTGRES_SSLMODE_ALLOW_INSECURE"] != "true" {
			t.Fatalf("%s %s POSTGRES_SSLMODE_ALLOW_INSECURE = %q, want true for AP Tailscale Postgres", composeFile, service, env["POSTGRES_SSLMODE_ALLOW_INSECURE"])
		}
		for _, key := range []string{"IRIS_WEBHOOK_TOKEN", "IRIS_BOT_TOKEN"} {
			if _, ok := env[key]; ok {
				t.Fatalf("%s %s rendered with Iris egress token %s", composeFile, service, key)
			}
		}
	}
}

func assertAPComposeDoesNotRequireCentralEgressEnvFiles(t *testing.T, cfg renderedCompose, composeFile string) {
	t.Helper()

	for _, service := range []string{"hololive-bot", "hololive-alarm-worker"} {
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

func assertRenderedPort(t *testing.T, cfg renderedCompose, service, hostIP, published, target, protocol string) {
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
	Source string
	Target string
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
			Source: stringValue(volumeMap["source"]),
			Target: stringValue(volumeMap["target"]),
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
