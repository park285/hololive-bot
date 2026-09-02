package settings

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

func TestRepoComposeProdHardenedDefaults(t *testing.T) {
	content := readRepoFile(t, composeProdFile)

	assertProdComposeDisallowedPatterns(t, content)
	assertProdComposeRequiredPatterns(t, content)
	assertProdComposeEgressEnvFiles(t, content)
	assertProdComposeNonEgressIsolation(t, content)
}

func TestRepoComposeProdPreservesExplicitBlankACLValues(t *testing.T) {
	cfg := renderComposeConfigWithEnvOverrides(t, map[string]string{
		kakaoACLEnabledEnv: "",
		kakaoACLModeEnv:    "",
	}, composeProdFile)

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
		env := composeEnvironment(t, cfg, service)

		for _, key := range []string{kakaoACLEnabledEnv, kakaoACLModeEnv} {
			if env[key] != "" {
				t.Fatalf("%s %s = %q, want explicit blank preserved", service, key, env[key])
			}
		}
	}
}

func TestRepoComposeCollectorDropsRetiredCompatAliases(t *testing.T) {
	for _, name := range []string{
		"YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES",
		"YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES",
		"YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS",
		"YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS",
	} {
		unsetEnvForTest(t, name)
	}

	t.Setenv("YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES", "4096")
	t.Setenv("YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS", "11")

	cfg := renderComposeConfig(t, composeProdFile)
	env := composeEnvironment(t, cfg, load.RuntimeYouTubeCollector)

	if got := env["YOUTUBE_COLLECTOR_MAX_AGGREGATE_BYTES"]; got != "" {
		t.Fatalf("retired aggregate bytes still rendered: %q", got)
	}

	if got := env["YOUTUBE_COLLECTOR_YOUTUBEJS_TIMEOUT_SECONDS"]; got != "" {
		t.Fatalf("retired youtubejs timeout still rendered: %q", got)
	}

	if got := env["YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES"]; got != "1048576" {
		t.Fatalf("YOUTUBE_COLLECTOR_MAX_SUCCESS_RESPONSE_BYTES = %q, want documented default", got)
	}

	if got := env["YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS"]; got != "30" {
		t.Fatalf("YOUTUBE_COLLECTOR_YOUTUBEJS_REQUEST_TIMEOUT_SECONDS = %q, want documented default", got)
	}
}

func assertProdComposeDisallowedPatterns(t *testing.T, content string) {
	t.Helper()

	disallowed := []string{
		"100.100.1.3",
		"${VALKEY_PORT_BIND_IP:-100.100.1.3}:6379:6379",
		"${ADMIN_DASHBOARD_PORT_BIND_IP:-100.100.1.3}:30190:30190",
		"${HOLOLIVE_BOT_PORT_BIND_IP:-100.100.1.3}:30001:30001",
		"network_mode: host",
		"/etc/stack-secrets/hololive-bot/certs:/run/hololive-bot/certs:ro",
		"POSTGRES_HOST: host.docker.internal",
		"POSTGRES_PORT: \"5433\"",
		"POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-require}",
		"IRIS_BASE_URL_FILE: ${IRIS_BASE_URL_FILE:-/app/runtime-config/iris_base_url}",
		"http://100.100.1.3:30190",
		"PGSSLMODE: \"require\"",
		"--unixsocketperm 777",
		"mode=0777",
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

	if got := strings.Count(content, "POSTGRES_HOST: ${HOLOLIVE_CENTRAL_POSTGRES_HOST:-holo-postgres}"); got != 1 {
		t.Fatalf("docker-compose.prod.yml POSTGRES_HOST holo-postgres-default anchor count = %d, want 1", got)
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
		"  POSTGRES_PORT: \"${HOLOLIVE_CENTRAL_POSTGRES_PORT:-5432}\"",
		"  POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-verify-full}",
		"  IRIS_BASE_URL_FILE: ${IRIS_BASE_URL_FILE:-}",
		"--unixsocketperm 660",
		"o: size=1m,mode=0770,uid=999,gid=1000",
	}
	for _, pattern := range required {
		if !strings.Contains(content, pattern) {
			t.Fatalf("docker-compose.prod.yml missing required pattern %q", pattern)
		}
	}

	appAnchor := topLevelYAMLBlock(t, content, "x-app-service:")
	if strings.Contains(appAnchor, "env_file:") {
		t.Fatal("x-app-service still defines env_file")
	}
}

func assertProdComposeEgressEnvFiles(t *testing.T, content string) {
	t.Helper()

	egressOwners := []string{serviceHololiveAPI, serviceAlarmWorker}
	for _, service := range egressOwners {
		block := composeServiceBlock(t, content, service)
		wantEnvFile := map[string]string{
			serviceHololiveAPI: "${HOLOLIVE_API_ENV_FILE:-/etc/stack-secrets/hololive-bot/bot.env}",
			serviceAlarmWorker: "${HOLOLIVE_ALARM_WORKER_ENV_FILE:-/etc/stack-secrets/hololive-bot/alarm-worker.env}",
		}[service]

		if !strings.Contains(block, "env_file:") || !strings.Contains(block, wantEnvFile) {
			t.Fatalf("%s must use per-service env_file %q for app-only secrets", service, wantEnvFile)
		}

		if strings.Contains(block, "/etc/stack-secrets/hololive-bot/env") || strings.Contains(block, "COMPOSE_ENV_FILE") {
			t.Fatalf("%s must not consume monolithic COMPOSE_ENV_FILE as env_file", service)
		}

		if !strings.Contains(block, "*iris-env") {
			t.Fatalf("%s must keep x-iris-env", service)
		}
	}
}

func assertProdComposeNonEgressIsolation(t *testing.T, content string) {
	t.Helper()

	nonEgress := []string{load.RuntimeYouTubeCollector, serviceAdminDashboard}
	for _, service := range nonEgress {
		block := composeServiceBlock(t, content, service)
		assertNonEgressEnvFilePolicy(t, service, block)

		for _, pattern := range []string{"*iris-env", irisWebhookTokenEnv, irisBotTokenEnv} {
			if strings.Contains(block, pattern) {
				t.Fatalf("%s contains Iris egress pattern %q", service, pattern)
			}
		}

		if service != serviceAdminDashboard {
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

	if service == load.RuntimeYouTubeCollector {
		if !strings.Contains(block, "${HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE:-/etc/stack-secrets/hololive-bot/youtube-collector.env}") {
			t.Fatal("youtube-collector must inject secrets via the scoped youtube-collector.env env_file")
		}

		return
	}

	if service != serviceAdminDashboard {
		if strings.Contains(block, "env_file:") {
			t.Fatalf("%s must not define env_file in hardened docker-compose.prod.yml", service)
		}

		return
	}

	if !strings.Contains(block, "${ADMIN_DASHBOARD_ENV_FILE:-/etc/stack-secrets/hololive-bot/admin-dashboard.env}") {
		t.Fatal("admin-dashboard must inject its secrets via the scoped admin-dashboard.env env_file")
	}

	if strings.Contains(block, "/etc/stack-secrets/hololive-bot/env") || strings.Contains(block, "COMPOSE_ENV_FILE") {
		t.Fatal("admin-dashboard must not consume monolithic COMPOSE_ENV_FILE as env_file")
	}
}

func TestRepoComposeProdRenderedIsolation(t *testing.T) {
	cfg := renderComposeConfig(t, composeProdFile)

	assertProdRenderedPostgresIsolation(t, cfg)
	assertProdRenderedValkeySocketIsolation(t, cfg)
	assertCollectorRenderedWithoutValkey(t, cfg, load.RuntimeYouTubeCollector) // CFG-006
	assertCollectorRenderedWithoutUnusedScraperEnv(t, cfg, load.RuntimeYouTubeCollector)
	assertValkeyConsumersUnchanged(t, cfg) // CFG-009
	assertProdRenderedNonEgressSecretIsolation(t, cfg)
	assertProdRenderedEgressRuntimeKeys(t, cfg)
	assertProdRenderedScopedProducerKeys(t, cfg)
	assertProdRenderedNoRuntimeConfigMount(t, cfg)
	assertProdRenderedPortAndCertScope(t, cfg)
}

func assertProdRenderedValkeySocketIsolation(t *testing.T, cfg renderedCompose) {
	t.Helper()

	if command := composeCommand(t, cfg, "valkey-cache"); !strings.Contains(command, "--unixsocketperm 660") || strings.Contains(command, "--unixsocketperm 777") {
		t.Fatalf("valkey-cache command = %q, want group-only unix socket permission", command)
	}

	volume, ok := cfg.Volumes["valkey-cache-socket"]
	if !ok {
		t.Fatal("rendered Compose missing valkey-cache-socket volume")
	}

	driverOpts, ok := volume["driver_opts"].(map[string]any)
	if !ok {
		t.Fatalf("valkey-cache-socket driver_opts has unexpected type %T", volume["driver_opts"])
	}

	if got := stringValue(driverOpts["o"]); got != "size=1m,mode=0770,uid=999,gid=1000" {
		t.Fatalf("valkey-cache-socket tmpfs opts = %q, want private shared-group directory", got)
	}

	wantConsumers := map[string]bool{
		"valkey-cache":     true,
		serviceHololiveAPI: true,
		serviceAlarmWorker: true,
	}

	for service := range cfg.Services {
		mountsSocket := false

		for _, volume := range composeVolumes(t, cfg, service) {
			if volume.Source == "valkey-cache-socket" && volume.Target == "/var/run/valkey" {
				mountsSocket = true
			}
		}

		hasSharedGroup := slices.Contains(composeGroupAdd(t, cfg, service), "1000")
		if mountsSocket != wantConsumers[service] {
			t.Fatalf("%s socket mount = %v, want %v", service, mountsSocket, wantConsumers[service])
		}

		if hasSharedGroup != mountsSocket {
			t.Fatalf("%s shared Valkey group = %v, socket mount = %v", service, hasSharedGroup, mountsSocket)
		}
	}
}

func assertCollectorRenderedWithoutValkey(t *testing.T, cfg renderedCompose, service string) {
	t.Helper()

	env := composeEnvironment(t, cfg, service)
	for key := range env {
		if strings.HasPrefix(key, "CACHE_") {
			t.Fatalf("%s rendered CACHE env %s=%q", service, key, env[key])
		}
	}

	for _, volume := range composeVolumes(t, cfg, service) {
		if volume.Source == "valkey-cache-socket" || volume.Target == "/var/run/valkey" {
			t.Fatalf("%s still mounts Valkey socket %+v", service, volume)
		}
	}

	if _, ok := composeDependsOn(t, cfg, service)["valkey-cache"]; ok {
		t.Fatalf("%s still depends_on valkey-cache", service)
	}
}

func assertCollectorRenderedWithoutUnusedScraperEnv(t *testing.T, cfg renderedCompose, service string) {
	t.Helper()

	env := composeEnvironment(t, cfg, service)
	for key := range env {
		if strings.HasPrefix(key, "SCRAPER_POLL_") || key == "SCRAPER_FETCHER_ENGINE" {
			t.Fatalf("%s rendered unused scraper env %s=%q", service, key, env[key])
		}
	}
}

func assertValkeyConsumersUnchanged(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
		env := composeEnvironment(t, cfg, service)
		if env["CACHE_HOST"] != "valkey-cache" {
			t.Fatalf("%s CACHE_HOST = %q, want valkey-cache", service, env["CACHE_HOST"])
		}

		if _, ok := composeDependsOn(t, cfg, service)["valkey-cache"]; !ok {
			t.Fatalf("%s missing depends_on valkey-cache", service)
		}

		mounted := false

		for _, volume := range composeVolumes(t, cfg, service) {
			if volume.Source == "valkey-cache-socket" && volume.Target == "/var/run/valkey" {
				mounted = true
			}
		}

		if !mounted {
			t.Fatalf("%s missing valkey-cache-socket mount", service)
		}
	}

	adminEnv := composeEnvironment(t, cfg, serviceAdminDashboard)
	if !strings.Contains(adminEnv["VALKEY_URL"], "valkey-cache") {
		t.Fatalf("admin-dashboard VALKEY_URL = %q, want valkey-cache consumer", adminEnv["VALKEY_URL"])
	}
}

func assertProdRenderedPostgresIsolation(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{serviceHoloPostgres, "hololive-db-migrate"} {
		if got := stringValue(composeService(t, cfg, service)["network_mode"]); got == "host" {
			t.Fatalf("%s rendered with network_mode=host", service)
		}
	}

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker, load.RuntimeYouTubeCollector} {
		env := composeEnvironment(t, cfg, service)
		if env["POSTGRES_HOST"] != serviceHoloPostgres {
			t.Fatalf("%s POSTGRES_HOST = %q, want holo-postgres", service, env["POSTGRES_HOST"])
		}

		if env["POSTGRES_PORT"] != "5432" {
			t.Fatalf("%s POSTGRES_PORT = %q, want 5432", service, env["POSTGRES_PORT"])
		}

		if env["POSTGRES_SSLMODE"] != load.PostgresSSLModeVerifyFull {
			t.Fatalf("%s POSTGRES_SSLMODE = %q, want verify-full", service, env["POSTGRES_SSLMODE"])
		}
	}
}

func assertProdRenderedNonEgressSecretIsolation(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{load.RuntimeYouTubeCollector, serviceAdminDashboard} {
		env := composeEnvironment(t, cfg, service)

		for _, key := range []string{irisWebhookTokenEnv, irisBotTokenEnv} {
			if _, ok := env[key]; ok {
				t.Fatalf("%s rendered with %s", service, key)
			}
		}

		if service != serviceAdminDashboard {
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

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
		env := composeEnvironment(t, cfg, service)

		for _, key := range []string{
			"KAKAO_ROOMS",
			kakaoACLEnabledEnv,
			kakaoACLModeEnv,
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

	for _, service := range []string{load.RuntimeYouTubeCollector} {
		env := composeEnvironment(t, cfg, service)
		if _, ok := env["API_SECRET_KEY"]; ok {
			t.Fatalf("%s must not receive admin API_SECRET_KEY", service)
		}

		if env["METRICS_API_KEY"] != "dummy" {
			t.Fatalf("%s METRICS_API_KEY = %q, want scoped env_file value", service, env["METRICS_API_KEY"])
		}

		if env["HOLOLIVE_HTTP_TRANSPORTS"] != "h3" {
			t.Fatalf("%s HOLOLIVE_HTTP_TRANSPORTS = %q, want h3", service, env["HOLOLIVE_HTTP_TRANSPORTS"])
		}
	}

	for _, service := range []string{load.RuntimeYouTubeCollector} {
		env := composeEnvironment(t, cfg, service)

		for _, key := range []string{"HOLODEX_API_KEY", "HOLODEX_API_KEY_1"} {
			if _, ok := env[key]; !ok {
				t.Fatalf("%s missing scoped %s mapping", service, key)
			}
		}
	}

	collectorEnv := composeEnvironment(t, cfg, load.RuntimeYouTubeCollector)
	if collectorEnv["POSTGRES_USER"] != "hololive_scraper" {
		t.Fatalf("youtube-collector POSTGRES_USER = %q, want hololive_scraper", collectorEnv["POSTGRES_USER"])
	}
}

func assertProdRenderedNoRuntimeConfigMount(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
		env := composeEnvironment(t, cfg, service)
		if env["IRIS_BASE_URL_FILE"] != "" {
			t.Fatalf("%s IRIS_BASE_URL_FILE = %q, want empty default", service, env["IRIS_BASE_URL_FILE"])
		}
	}

	for _, service := range []string{load.RuntimeYouTubeCollector, serviceAdminDashboard} {
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
		serviceHololiveAPI:           true,
		serviceAlarmWorker:           true,
		load.RuntimeYouTubeCollector: true,
	}

	for serviceName, service := range cfg.Services {
		for _, port := range composePorts(t, serviceName, service) {
			if port.HostIP != "" && port.HostIP != "127.0.0.1" && port.HostIP != "::1" && port.HostIP != "localhost" {
				t.Fatalf("%s publishes non-loopback port %+v", serviceName, port)
			}
		}

		for _, target := range composeVolumeTargets(t, cfg, serviceName) {
			if target == runtimeCertsDir {
				t.Fatalf("%s still mounts the broad cert directory", serviceName)
			}

			if strings.HasSuffix(target, ".key") && !h3KeyConsumers[serviceName] {
				t.Fatalf("%s mounts private key file %s", serviceName, target)
			}
		}
	}
}
