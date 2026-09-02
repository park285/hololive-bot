package settings

import (
	"strings"
	"testing"
)

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
			cfg := renderAPComposeConfig(t, composeProdFile, renderableAPComposeFile(t, tt.file))
			assertAPComposeCertMountsAreMinimized(t, cfg, tt.file)
			assertAPComposeDoesNotRequireCentralEgressEnvFiles(t, cfg, tt.file)

			for _, service := range apComposeServiceNames(t, cfg, tt.file) {
				assertCollectorRenderedWithoutValkey(t, cfg, service) // CFG-007
				assertCollectorRenderedWithoutUnusedScraperEnv(t, cfg, service)
			}
		})
	}
}

func TestRepoComposeLiveCompatOverlayRestoresLiveWiringWithScopedNonEgress(t *testing.T) {
	overlay := readRepoFile(t, composeLiveCompatFile)
	assertLiveCompatOverlayText(t, overlay)

	cfg := renderComposeConfig(t, composeProdFile, composeLiveCompatFile)

	assertLiveCompatRenderedPortsAndModes(t, cfg)
	assertLiveCompatRenderedPostgres(t, cfg)
	assertCollectorRenderedWithoutValkey(t, cfg, runtimeYouTubeCollector) // CFG-007
	assertCollectorRenderedWithoutUnusedScraperEnv(t, cfg, runtimeYouTubeCollector)
	assertValkeyConsumersUnchanged(t, cfg) // CFG-009
	assertLiveCompatRenderedSecrets(t, cfg)
	assertLiveCompatRenderedRuntimeConfig(t, cfg)
}

func assertLiveCompatOverlayText(t *testing.T, overlay string) {
	t.Helper()

	apiBlock := composeServiceBlock(t, overlay, serviceHololiveAPI)
	if strings.Contains(apiBlock, "${HOLOLIVE_SHORT_LINK_ADDR") || !strings.Contains(apiBlock, `HOLOLIVE_SHORT_LINK_ADDR: ":30101"`) {
		t.Fatal("live overlay must pin HOLOLIVE_SHORT_LINK_ADDR to the published ingress port")
	}

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
		block := composeServiceBlock(t, overlay, service)
		wantEnvFile := map[string]string{
			serviceHololiveAPI: "${HOLOLIVE_API_ENV_FILE:-/etc/stack-secrets/hololive-bot/bot.env}",
			serviceAlarmWorker: "${HOLOLIVE_ALARM_WORKER_ENV_FILE:-/etc/stack-secrets/hololive-bot/alarm-worker.env}",
		}[service]

		if !strings.Contains(block, "env_file:") || !strings.Contains(block, wantEnvFile) {
			t.Fatalf("live overlay must keep per-service env_file %q for %s", wantEnvFile, service)
		}

		if strings.Contains(block, "/etc/stack-secrets/hololive-bot/env") || strings.Contains(block, "COMPOSE_ENV_FILE") {
			t.Fatalf("live overlay must not restore monolithic env_file for %s", service)
		}
	}

	for _, service := range []string{runtimeYouTubeCollector, serviceAdminDashboard} {
		block := composeServiceBlock(t, overlay, service)
		if strings.Contains(block, "env_file:") {
			t.Fatalf("live overlay must keep nonEgress %s scoped without env_file", service)
		}
	}

	valkeyBlock := composeServiceBlock(t, overlay, "valkey-cache")
	if strings.Contains(valkeyBlock, "command:") {
		t.Fatal("live overlay must inherit valkey command from prod")
	}

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
		block := composeServiceBlock(t, overlay, service)
		if !strings.Contains(block, "IRIS_BASE_URL_ALLOWED_HOSTS: ${IRIS_BASE_URL_ALLOWED_HOSTS:-100.100.1.5}") {
			t.Fatalf("docker-compose.live-compat.yml missing IRIS_BASE_URL_ALLOWED_HOSTS default for %s", service)
		}
	}
}

func assertLiveCompatRenderedPortsAndModes(t *testing.T, cfg renderedCompose) {
	t.Helper()

	assertRenderedPort(t, cfg, "valkey-cache", "6379", "6379", "tcp")
	assertRenderedPortOnHost(t, cfg, serviceAdminDashboard, "127.0.0.1", "30190", "30190", "tcp")
	assertRenderedPort(t, cfg, serviceHoloPostgres, "5433", "5432", "tcp")
	assertRenderedPort(t, cfg, serviceHololiveAPI, "30001", "30001", "tcp")
	assertRenderedPort(t, cfg, serviceHololiveAPI, "30001", "30001", "udp")
	assertRenderedPortOnHost(t, cfg, serviceHololiveAPI, "127.0.0.1", "30101", "30101", "tcp")

	apiEnv := composeEnvironment(t, cfg, serviceHololiveAPI)
	if apiEnv["HOLOLIVE_SHORT_LINK_ADDR"] != ":30101" {
		t.Fatalf("hololive-api HOLOLIVE_SHORT_LINK_ADDR = %q, want :30101", apiEnv["HOLOLIVE_SHORT_LINK_ADDR"])
	}

	if command := composeCommand(t, cfg, "valkey-cache"); !strings.Contains(command, "--unixsocketperm 660") {
		t.Fatalf("live overlay valkey command = %q, want --unixsocketperm 660", command)
	}

	for _, service := range []string{serviceHoloPostgres, "hololive-db-migrate"} {
		if got := stringValue(composeService(t, cfg, service)["network_mode"]); got == "host" {
			t.Fatalf("%s network_mode = %q, want bridge networking", service, got)
		}
	}
}

func assertLiveCompatRenderedPostgres(t *testing.T, cfg renderedCompose) {
	t.Helper()

	migrationEnv := composeEnvironment(t, cfg, "hololive-db-migrate")
	if migrationEnv["PGHOST"] != serviceHoloPostgres || migrationEnv["PGPORT"] != "5432" {
		t.Fatalf("hololive-db-migrate PGHOST/PGPORT = %q/%q, want holo-postgres/5432", migrationEnv["PGHOST"], migrationEnv["PGPORT"])
	}

	postgresEnv := composeEnvironment(t, cfg, serviceHoloPostgres)
	if postgresEnv["PGPORT"] != "5432" {
		t.Fatalf("holo-postgres PGPORT = %q, want 5432", postgresEnv["PGPORT"])
	}

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker, runtimeYouTubeCollector} {
		assertLiveCompatRenderedPostgresService(t, cfg, service)
	}
}

func assertLiveCompatRenderedPostgresService(t *testing.T, cfg renderedCompose, service string) {
	t.Helper()

	env := composeEnvironment(t, cfg, service)
	if env["POSTGRES_HOST"] != serviceHoloPostgres || env["POSTGRES_PORT"] != "5432" || env["POSTGRES_SSLMODE"] != postgresSSLModeVerifyFull {
		t.Fatalf("%s POSTGRES env = %q/%q/%q, want holo-postgres/5432/verify-full", service, env["POSTGRES_HOST"], env["POSTGRES_PORT"], env["POSTGRES_SSLMODE"])
	}

	if value, ok := env["POSTGRES_SSLMODE_ALLOW_INSECURE"]; ok {
		t.Fatalf("%s renders retired POSTGRES_SSLMODE_ALLOW_INSECURE=%q; verify-full replaced the downgrade path", service, value)
	}

	if env["POSTGRES_SSLROOTCERT"] != postgresCACertPath {
		t.Fatalf("%s POSTGRES_SSLROOTCERT = %q, want /run/hololive-bot/certs/postgres-ca.pem", service, env["POSTGRES_SSLROOTCERT"])
	}

	assertLiveCompatVolumeTargets(t, cfg, service)
}

func assertLiveCompatVolumeTargets(t *testing.T, cfg renderedCompose, service string) {
	t.Helper()

	targets := strings.Join(composeVolumeTargets(t, cfg, service), "\n")
	required := []string{"/app/data", "/app/logs", runtimeCertsDir}

	if service != runtimeYouTubeCollector {
		required = append(required, "/app/runtime-config", "/var/run/valkey")
	}

	for _, target := range required {
		if !strings.Contains(targets, target) {
			t.Fatalf("%s missing live-compat volume target %s in %q", service, target, targets)
		}
	}

	if service == runtimeYouTubeCollector && strings.Contains(targets, "/var/run/valkey") {
		t.Fatal("youtube-collector live-compat still mounts Valkey socket")
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

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
		env := composeEnvironment(t, cfg, service)

		for _, key := range []string{irisWebhookTokenEnv, irisBotTokenEnv} {
			if _, ok := env[key]; !ok {
				t.Fatalf("%s missing env_file-restored key %s", service, key)
			}
		}
	}
}

func assertLiveCompatNonEgressSecrets(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{runtimeYouTubeCollector, serviceAdminDashboard} {
		env := composeEnvironment(t, cfg, service)

		for _, key := range []string{irisWebhookTokenEnv, irisBotTokenEnv} {
			if _, ok := env[key]; ok {
				t.Fatalf("nonEgress %s rendered with %s under live overlay", service, key)
			}
		}

		if service != serviceAdminDashboard {
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

	dashboardEnv := composeEnvironment(t, cfg, serviceAdminDashboard)
	if strings.Contains(dashboardEnv["ALLOWED_ORIGINS"], "100.100.1.3:30190") {
		t.Fatalf("admin-dashboard ALLOWED_ORIGINS = %q, want no default Tailnet origin", dashboardEnv["ALLOWED_ORIGINS"])
	}

	if !strings.Contains(dashboardEnv["ALLOWED_ORIGINS"], "https://admin.holoshi.com") {
		t.Fatalf("admin-dashboard ALLOWED_ORIGINS = %q, want explicit HTTPS admin origin", dashboardEnv["ALLOWED_ORIGINS"])
	}
}

func assertLiveCompatRenderedRuntimeConfig(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
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

func TestRepoComposeMainAPLiveCompatOverlayRestoresExtendedProducer(t *testing.T) {
	assertMainAPLiveCompatOverlayText(t)

	cfg := renderComposeConfig(t,
		composeProdFile,
		composeLiveCompatFile,
		"deploy/compose/docker-compose.main-ap.yml",
		"deploy/compose/docker-compose.main-ap.live-compat.yml",
	)

	assertMainAPLiveCompatRenderedEgressAllowedHosts(t, cfg)
	assertMainAPLiveCompatRenderedProducer(t, cfg)
	assertCollectorRenderedWithoutValkey(t, cfg, runtimeYouTubeCollector) // CFG-007
	assertCollectorRenderedWithoutUnusedScraperEnv(t, cfg, runtimeYouTubeCollector)
}

func TestCFG010ExactRevisionRollbackDocs(t *testing.T) {
	t.Parallel()

	collectorRunbook := readRepoFile(t, "docs/current/runbooks/youtube-collector.md")

	for _, token := range []string{
		"collector Go binary/image",
		"bundled Node helper/package-lock",
		"Compose base and AP overlays",
		"host-native env generator/wrapper",
		"Schema/data rollback is none",
		"rollback.md",
	} {
		if !strings.Contains(collectorRunbook, token) {
			t.Fatalf("youtube-collector runbook missing CFG-010 rollback unit %q", token)
		}
	}

	if strings.Contains(collectorRunbook, "Valkey") {
		t.Fatal("youtube-collector runbook must point at rollback.md instead of restating Valkey topology")
	}

	rollback := readRepoFile(t, "docs/current/runbooks/rollback.md")

	for _, token := range []string{
		"Binary-only Valkey rollback",
		"exact repository revision",
		"youtube-collector.md#rollback",
	} {
		if !strings.Contains(rollback, token) {
			t.Fatalf("rollback.md missing CFG-010 Valkey topology unit %q", token)
		}
	}
}

func assertMainAPLiveCompatOverlayText(t *testing.T) {
	t.Helper()

	prod := readRepoFile(t, composeProdFile)

	const collectorEnvFile = "${HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE:-/etc/stack-secrets/hololive-bot/youtube-collector.env}"

	if block := composeServiceBlock(t, prod, runtimeYouTubeCollector); !strings.Contains(block, "env_file:") || !strings.Contains(block, collectorEnvFile) {
		t.Fatalf("prod must give youtube-collector scoped env_file %q", collectorEnvFile)
	}
}

func assertMainAPLiveCompatRenderedEgressAllowedHosts(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
		env := composeEnvironment(t, cfg, service)
		if env["IRIS_BASE_URL_ALLOWED_HOSTS"] != "100.100.1.5" {
			t.Fatalf("%s IRIS_BASE_URL_ALLOWED_HOSTS = %q, want 100.100.1.5", service, env["IRIS_BASE_URL_ALLOWED_HOSTS"])
		}
	}
}

func assertMainAPLiveCompatRenderedProducer(t *testing.T, cfg renderedCompose) {
	t.Helper()

	env := composeEnvironment(t, cfg, runtimeYouTubeCollector)
	if env["POSTGRES_HOST"] != serviceHoloPostgres || env["POSTGRES_PORT"] != "5432" || env["POSTGRES_SSLMODE"] != postgresSSLModeVerifyFull {
		t.Fatalf("youtube-collector POSTGRES env = %q/%q/%q, want holo-postgres/5432/verify-full", env["POSTGRES_HOST"], env["POSTGRES_PORT"], env["POSTGRES_SSLMODE"])
	}

	if value, ok := env["POSTGRES_SSLMODE_ALLOW_INSECURE"]; ok {
		t.Fatalf("youtube-collector renders retired POSTGRES_SSLMODE_ALLOW_INSECURE=%q", value)
	}

	if env["POSTGRES_SSLROOTCERT"] != postgresCACertPath {
		t.Fatalf("youtube-collector POSTGRES_SSLROOTCERT = %q, want /run/hololive-bot/certs/postgres-ca.pem", env["POSTGRES_SSLROOTCERT"])
	}

	for _, key := range []string{irisWebhookTokenEnv, irisBotTokenEnv} {
		if _, ok := env[key]; ok {
			t.Fatalf("youtube-collector rendered with %s under live overlay", key)
		}
	}

	if _, ok := env["API_SECRET_KEY"]; ok {
		t.Fatal("youtube-collector must not receive admin API_SECRET_KEY under live overlay")
	}

	for _, key := range []string{"METRICS_API_KEY", "HOLODEX_API_KEY", "HOLODEX_API_KEY_1"} {
		if _, ok := env[key]; !ok {
			t.Fatalf("youtube-collector missing scoped %s mapping", key)
		}
	}

	for _, key := range []string{"HOLODEX_API_KEY_2", "SCRAPER_PROXY_ENABLED", "YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT", "YOUTUBE_ENABLE_QUOTA_BUILDING"} {
		if _, ok := env[key]; !ok {
			t.Fatalf("youtube-collector missing producer env_file key %s", key)
		}
	}

	targets := strings.Join(composeVolumeTargets(t, cfg, runtimeYouTubeCollector), "\n")

	for _, target := range []string{"/app/data", "/app/logs", runtimeCertsDir} {
		if !strings.Contains(targets, target) {
			t.Fatalf("youtube-collector missing live-compat volume target %s in %q", target, targets)
		}
	}

	if strings.Contains(targets, "/var/run/valkey") {
		t.Fatal("youtube-collector live-compat still mounts Valkey socket")
	}
}
