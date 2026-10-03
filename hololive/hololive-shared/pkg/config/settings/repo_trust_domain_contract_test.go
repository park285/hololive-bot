package settings

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
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

	assertHololiveAPIPlaneAuthRequired(t)
	assertHololiveAPIHasNoNativeExecutionImports(t)
	assertHololiveAPITemplateInterpretationContract(t)
	assertHololiveAPITrustDomainDecisionDocumented(t)
}

func assertHololiveAPIPlaneAuthRequired(t *testing.T) {
	t.Helper()

	for path, marker := range map[string]string{
		"hololive/hololive-api/internal/planes/admin/internal/httpapi/router.go":       "API_SECRET_KEY required",
		"hololive/hololive-api/internal/planes/llm/runtime/bootstrap_llm_scheduler.go": "API_SECRET_KEY required",
	} {
		if content := readRepoFile(t, path); !strings.Contains(content, marker) {
			t.Fatalf("%s missing plane auth requirement %q", path, marker)
		}
	}
}

func assertHololiveAPIHasNoNativeExecutionImports(t *testing.T) {
	t.Helper()

	forbidden := map[string]bool{"os/exec": true, "plugin": true}

	for _, relativeRoot := range []string{
		"hololive/hololive-api",
		"hololive/hololive-shared/pkg/service/template",
	} {
		root := filepath.Join(repoRootFromConfigTest(t), filepath.FromSlash(relativeRoot))
		assertNoNativeExecutionImports(t, root, forbidden)
	}
}

func assertNoNativeExecutionImports(t *testing.T, root string, forbidden map[string]bool) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse imports from %s: %w", path, err)
		}

		for _, imported := range file.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return fmt.Errorf("unquote import in %s: %w", path, err)
			}

			if forbidden[name] {
				return fmt.Errorf("production trust-domain path imports native execution package %q in %s", name, path)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertHololiveAPITemplateInterpretationContract(t *testing.T) {
	t.Helper()

	for path, markers := range map[string][]string{
		"hololive/hololive-api/internal/planes/admin/internal/httpapi/registration.go": {
			"holoAPI.Use(middleware.APIKeyAuthMiddleware(apiKey))",
		},
		"hololive/hololive-api/internal/planes/admin/internal/httpapi/routes.go": {
			`holoAPI.PUT("/templates/:key", handler.UpsertTemplate)`,
			`holoAPI.POST("/templates/:key/preview", handler.PreviewTemplate)`,
		},
		"hololive/hololive-api/internal/planes/admin/internal/server/api/api_template.go": {
			"h.templateAdmin.Save(ctx, key, channelPtr, req.Body)",
			"h.templateAdmin.Preview(ctx, key, req.Body)",
		},
		"hololive/hololive-shared/pkg/service/template/admin_service.go": {
			".Parse(body)",
			"tmpl.Execute(&buf, sampleData)",
		},
		"hololive/hololive-shared/pkg/service/template/renderer.go": {
			"tmpl, err := r.getTemplate(ctx, key, channelID)",
			"tmpl.Execute(&buf, data)",
		},
		"hololive/hololive-shared/pkg/service/template/renderer_load.go": {
			"r.pool.QueryRow(ctx, mustSQL(\"renderer_resolve.sql\")",
			"r.pool.QueryRow(ctx, mustSQL(\"renderer_load.sql\")",
			".Parse(body)",
		},
	} {
		content := readRepoFile(t, path)

		for _, marker := range markers {
			if !strings.Contains(content, marker) {
				t.Fatalf("%s missing template interpretation contract marker %q", path, marker)
			}
		}
	}
}

func assertHololiveAPITrustDomainDecisionDocumented(t *testing.T) {
	t.Helper()

	content := readRepoFile(t, "docs/current/architecture/hololive-api-trust-domain.md")

	for _, statement := range []string{
		"One process is one trust domain.",
		"A process-level compromise in any plane exposes credentials available to all three planes, including bot egress credentials.",
		"No admin or LLM endpoint may load native plugins, spawn processes, invoke shells, or execute",
		"Admin template update and preview are an explicit, authenticated, capability-bounded interpretation",
		"user-supplied Go `text/template` body를 parse하고 execute합니다.",
		"They do not provide native command, plugin, or process",
		"Split trigger: if admin-plane or LLM-plane compromise must not expose bot egress credentials",
	} {
		if !strings.Contains(content, statement) {
			t.Fatalf("hololive-api trust-domain decision missing %q", statement)
		}
	}
}
