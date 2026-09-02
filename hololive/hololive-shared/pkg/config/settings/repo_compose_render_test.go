package settings

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type renderedCompose struct {
	Services map[string]map[string]any `yaml:"services"`
	Volumes  map[string]map[string]any `yaml:"volumes"`
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

	return renderComposeConfigWithEnvFileAndOverrides(t, writeCentralComposeEnvFile(t), nil, files...)
}

func renderComposeConfigWithEnvOverrides(t *testing.T, overrides map[string]string, files ...string) renderedCompose {
	t.Helper()

	return renderComposeConfigWithEnvFileAndOverrides(t, writeCentralComposeEnvFile(t), overrides, files...)
}

func renderComposeConfigWithEnvFileAndOverrides(t *testing.T, composeEnvFile string, overrides map[string]string, files ...string) renderedCompose {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker CLI unavailable: %v", err)
	}

	ctx, cancel := dockerComposeConfigContext(t)
	defer cancel()

	cmd := dockerComposeConfigCommand(ctx, t, files)
	repoRoot := repoRootFromConfigTest(t)
	appEnvFile := writeCentralAppEnvFile(t)

	cmd.Dir = repoRoot

	strip := map[string]string{"HOLOLIVE_RUNTIME_GID": "1002"}
	maps.Copy(strip, overrides)

	cmd.Env = append(environmentWithoutKeys(os.Environ(), strip),
		"COMPOSE_ENV_FILE="+composeEnvFile,
		"HOLOLIVE_API_ENV_FILE="+appEnvFile,
		"HOLOLIVE_ALARM_WORKER_ENV_FILE="+appEnvFile,
		"HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE="+writeAPProducerEnvFile(t),
		"ADMIN_DASHBOARD_ENV_FILE="+writeAdminDashboardEnvFile(t),
		"DB_PASSWORD=dummy",
		"CACHE_PASSWORD=dummy",
		"IRIS_WEBHOOK_TOKEN=dummy",
		"IRIS_BOT_TOKEN=dummy",
		"ADMIN_PASS_BCRYPT=dummy",
		"SESSION_SECRET=dummy",
		"LIVE_LOGS_PATH=/srv/hololive-logs-dummy",
		"HOLOLIVE_RUNTIME_GID=1002",
		"HOLO_API_VERSION="+strings.TrimSpace(readRepoFile(t, "hololive/hololive-api/VERSION")),
		"HOLO_ALARM_WORKER_VERSION="+strings.TrimSpace(readRepoFile(t, "hololive/hololive-alarm-worker/VERSION")),
	)

	for key, value := range overrides {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

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
		t.Fatal("rendered compose has no services")
	}

	return cfg
}

func environmentWithoutKeys(environment []string, excluded map[string]string) []string {
	if len(excluded) == 0 {
		return environment
	}

	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, skip := excluded[key]; skip {
				continue
			}
		}

		filtered = append(filtered, entry)
	}

	return filtered
}

func dockerComposeConfigCommand(ctx context.Context, t *testing.T, files []string) *exec.Cmd {
	t.Helper()

	switch strings.Join(files, "\x00") {
	case composeProdFile:
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "--profile", "main-ap", "-f", composeProdFile, "config")
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.live-compat.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "--profile", "main-ap", "-f", composeProdFile, "-f", composeLiveCompatFile, "config")
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.live-compat.yml\x00deploy/compose/docker-compose.main-ap.yml\x00deploy/compose/docker-compose.main-ap.live-compat.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "--profile", "main-ap", "-f", composeProdFile, "-f", composeLiveCompatFile, "-f", "deploy/compose/docker-compose.main-ap.yml", "-f", "deploy/compose/docker-compose.main-ap.live-compat.yml", "config")
	default:
		t.Fatalf("unsupported compose file set: %v", files)

		return nil
	}
}

func dockerComposeConfigContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	if deadline, ok := t.Deadline(); ok {
		return context.WithDeadline(t.Context(), deadline)
	}

	return context.WithTimeout(t.Context(), 30*time.Second)
}

func renderAPComposeConfig(t *testing.T, files ...string) renderedCompose {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker CLI unavailable: %v", err)
	}

	ctx, cancel := dockerComposeConfigContext(t)
	defer cancel()

	cmd := dockerAPComposeConfigCommand(ctx, t, files)
	repoRoot := repoRootFromConfigTest(t)

	cmd.Dir = repoRoot

	cmd.Env = append(environmentWithoutKeys(os.Environ(), map[string]string{
		"HOLOLIVE_RUNTIME_GID":        "1002",
		"HOLOLIVE_CENTRAL_CACHE_HOST": "omit",
		"HOLOLIVE_CENTRAL_CACHE_PORT": "omit",
	}),
		"COMPOSE_ENV_FILE="+writeAPComposeEnvFile(t),
		"HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE="+writeAPProducerEnvFile(t),
		"ADMIN_DASHBOARD_ENV_FILE="+writeAdminDashboardEnvFile(t),
		"DB_PASSWORD=dummy",
		"CACHE_PASSWORD=dummy",
		"ADMIN_PASS_BCRYPT=dummy",
		"SESSION_SECRET=dummy",
		"HOLOLIVE_CENTRAL_POSTGRES_HOST=stub",
		"CLIPROXY_BASE_URL=https://cliproxy.invalid",
		"SEOUL_METRICS_BIND_IP=100.100.1.5",
		"HOLOLIVE_RUNTIME_GID=1002",
		"HOLO_API_VERSION="+strings.TrimSpace(readRepoFile(t, "hololive/hololive-api/VERSION")),
		"HOLO_ALARM_WORKER_VERSION="+strings.TrimSpace(readRepoFile(t, "hololive/hololive-alarm-worker/VERSION")),
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

func dockerAPComposeConfigCommand(ctx context.Context, t *testing.T, files []string) *exec.Cmd {
	t.Helper()

	switch strings.Join(files, "\x00") {
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.osaka.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "-f", composeProdFile, "-f", "deploy/compose/docker-compose.osaka.yml", "config")
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.osaka2.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "-f", composeProdFile, "-f", "deploy/compose/docker-compose.osaka2.yml", "config")
	case "deploy/compose/docker-compose.prod.yml\x00deploy/compose/docker-compose.seoul.yml":
		return exec.CommandContext(ctx, "docker", "compose", "--profile", "oracle", "-f", composeProdFile, "-f", "deploy/compose/docker-compose.seoul.yml", "config")
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

// /etc/stack-secrets/hololive-bot/admin-dashboard.env는 0600 root 렌더 파일이라 kapu로 도는 테스트는
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
		"IRIS_WEBHOOK_TOKEN=dummy",
		"IRIS_BOT_TOKEN=dummy",
	})
}

func writeAPComposeEnvFile(t *testing.T) string {
	t.Helper()

	return writeTempEnvFile(t, "ap-compose-*.env", []string{
		"ADMIN_PASS_BCRYPT=dummy",
		"CACHE_PASSWORD=dummy",
		"DB_PASSWORD=dummy",
		"HOLOLIVE_CENTRAL_POSTGRES_HOST=dummy",
		"SESSION_SECRET=dummy",
	})
}

func renderableAPComposeFile(t *testing.T, relativePath string) string {
	t.Helper()

	return writeRenderableAPComposeFile(t, relativePath, readRepoFile(t, relativePath))
}

func writeRenderableAPComposeFile(t *testing.T, sourceName, content string) string {
	t.Helper()

	if strings.Contains(content, "/etc/stack-secrets/hololive-bot/env") || strings.Contains(content, "COMPOSE_ENV_FILE") {
		t.Fatalf("%s must not reference monolithic hololive env file", sourceName)
	}

	const producerEnvFile = "${HOLOLIVE_YOUTUBE_COLLECTOR_ENV_FILE:-/etc/stack-secrets/hololive-bot/youtube-collector.env}"

	if !strings.Contains(content, producerEnvFile) {
		t.Fatalf("%s missing AP youtube-collector env_file path %s", sourceName, producerEnvFile)
	}

	return sourceName
}

func writeAPProducerEnvFile(t *testing.T) string {
	t.Helper()

	return writeTempEnvFile(t, "youtube-collector-*.env", []string{
		"METRICS_API_KEY=dummy",
		"HOLODEX_API_KEY=dummy",
		"HOLODEX_API_KEY_1=dummy",
		"HOLODEX_API_KEY_2=dummy",
		"HOLODEX_API_KEY_3=dummy",
		"HOLODEX_API_KEY_4=dummy",
		"HOLODEX_API_KEY_5=dummy",
		"SCRAPER_PROXY_ENABLED=false",
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

	targets := make(map[string]bool)

	for _, volume := range composeVolumes(t, cfg, service) {
		targets[assertAPComposeVolumeSafe(t, composeFile, service, volume)] = true
	}

	if targets["/run/hololive-bot/certs/iris-ca.pem"] {
		t.Fatalf("%s %s must not mount iris-ca.pem; collector loader does not dial Iris", composeFile, service)
	}

	if !targets[postgresCACertPath] {
		t.Fatalf("%s %s missing postgres-ca.pem mount - verify-full needs the CA bundle over the Tailscale Postgres path", composeFile, service)
	}

	if !targets["/run/hololive-bot/certs/hololive-h3.crt"] || !targets[hololiveH3KeyPath] {
		t.Fatalf("%s %s missing hololive-h3 cert/key mounts", composeFile, service)
	}
}

func assertAPComposeVolumeSafe(t *testing.T, composeFile, service string, volume renderedVolume) string {
	t.Helper()

	source := cleanVolumePath(volume.Source)
	target := cleanVolumePath(volume.Target)

	if source == "/etc/stack-secrets/hololive-bot/certs" && target == runtimeCertsDir {
		t.Fatalf("%s %s mounts broad cert directory: source=%q target=%q", composeFile, service, volume.Source, volume.Target)
	}

	isH3ServerKey := source == "/etc/stack-secrets/hololive-bot/certs/hololive-h3.key" && target == hololiveH3KeyPath
	if (strings.HasSuffix(volume.Source, ".key") || strings.HasSuffix(volume.Target, ".key")) && !isH3ServerKey {
		t.Fatalf("%s %s mounts private key file: source=%q target=%q", composeFile, service, volume.Source, volume.Target)
	}

	return target
}

func assertAPComposeServiceEnvIsolation(t *testing.T, cfg renderedCompose, composeFile, service string) {
	t.Helper()

	env := composeEnvironment(t, cfg, service)
	if value, ok := env["POSTGRES_SSLMODE_ALLOW_INSECURE"]; ok {
		t.Fatalf("%s %s renders retired POSTGRES_SSLMODE_ALLOW_INSECURE=%q", composeFile, service, value)
	}

	if env["POSTGRES_SSLROOTCERT"] != postgresCACertPath {
		t.Fatalf("%s %s POSTGRES_SSLROOTCERT = %q, want /run/hololive-bot/certs/postgres-ca.pem", composeFile, service, env["POSTGRES_SSLROOTCERT"])
	}

	for _, key := range []string{irisWebhookTokenEnv, irisBotTokenEnv} {
		if _, ok := env[key]; ok {
			t.Fatalf("%s %s rendered with Iris egress token %s", composeFile, service, key)
		}
	}
}

func assertAPComposeDoesNotRequireCentralEgressEnvFiles(t *testing.T, cfg renderedCompose, composeFile string) {
	t.Helper()

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker} {
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
		if strings.HasPrefix(service, "youtube-collector-") {
			serviceNames = append(serviceNames, service)
		}
	}

	if len(serviceNames) == 0 {
		t.Fatalf("%s rendered no AP youtube-collector services", composeFile)
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

func composeGroupAdd(t *testing.T, cfg renderedCompose, service string) []string {
	t.Helper()

	raw, ok := composeService(t, cfg, service)["group_add"]
	if !ok {
		return nil
	}

	values, ok := raw.([]any)
	if !ok {
		t.Fatalf("%s group_add has unexpected type %T", service, raw)
	}

	groups := make([]string, 0, len(values))
	for _, value := range values {
		groups = append(groups, stringValue(value))
	}

	return groups
}

func composeDependsOn(t *testing.T, cfg renderedCompose, service string) map[string]any {
	t.Helper()

	raw, ok := composeService(t, cfg, service)["depends_on"]
	if !ok || raw == nil {
		return map[string]any{}
	}

	switch deps := raw.(type) {
	case map[string]any:
		return deps
	case []any:
		named := make(map[string]any, len(deps))
		for _, value := range deps {
			named[stringValue(value)] = struct{}{}
		}

		return named
	default:
		t.Fatalf("%s depends_on has unexpected type %T", service, raw)

		return nil
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
