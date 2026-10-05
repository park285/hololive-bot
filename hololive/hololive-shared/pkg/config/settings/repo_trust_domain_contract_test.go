package settings

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

func TestRepoRemoteBuildCacheExportsOnlyFinalImageLayers(t *testing.T) {
	content := readRepoFile(t, "deploy/compose/docker-compose.remote-cache.yml")

	if strings.Contains(content, "mode=max") {
		t.Fatal("remote cache overlay exports intermediate build layers with mode=max")
	}

	for _, service := range []string{
		serviceHololiveAPI,
		serviceAlarmWorker,
		runtimepolicy.RuntimeYouTubeCollector,
	} {
		block := composeServiceBlock(t, content, service)
		if got := strings.Count(block, "mode=min"); got != 1 {
			t.Fatalf("%s remote cache mode=min count = %d, want 1", service, got)
		}
	}
}

func TestRepoHololiveAPITrustDomainControls(t *testing.T) {
	cfg := renderComposeConfig(t, composeProdFile)
	service := composeService(t, cfg, serviceHololiveAPI)
	env := composeEnvironment(t, cfg, serviceHololiveAPI)

	for _, port := range []string{"30003", "30006"} {
		assertRenderedPortOnHost(t, cfg, serviceHololiveAPI, "127.0.0.1", port, port, "tcp")
		assertRenderedPortOnHost(t, cfg, serviceHololiveAPI, "127.0.0.1", port, port, "udp")
	}

	for key, want := range map[string]string{
		"HOLOLIVE_ADMIN_API_HTTP_TRANSPORTS":     "h3",
		"HOLOLIVE_ADMIN_API_H3_ADDR":             ":30006",
		"HOLOLIVE_LLM_SCHEDULER_HTTP_TRANSPORTS": "h3",
		"HOLOLIVE_LLM_SCHEDULER_H3_ADDR":         ":30003",
	} {
		if env[key] != want {
			t.Fatalf("hololive-api %s = %q, want %q", key, env[key], want)
		}
	}

	if strings.TrimSpace(env["API_SECRET_KEY"]) == "" {
		t.Fatal("hololive-api must receive API_SECRET_KEY for admin/LLM plane auth")
	}

	networks, ok := service["networks"].(map[string]any)
	if !ok {
		t.Fatalf("hololive-api networks has unexpected type %T", service["networks"])
	}

	if _, ok := networks["docker-proxy-net"]; ok {
		t.Fatal("hololive-api must not join docker-proxy-net")
	}

	for _, target := range composeVolumeTargets(t, cfg, serviceHololiveAPI) {
		if target == "/var/run/docker.sock" {
			t.Fatal("hololive-api must not mount the Docker socket")
		}
	}

	if _, ok := env["DOCKER_HOST"]; ok {
		t.Fatal("hololive-api must not receive DOCKER_HOST")
	}
}
