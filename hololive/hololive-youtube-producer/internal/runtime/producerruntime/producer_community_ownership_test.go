package producerruntime

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

func TestPostgresPoolNilSafe(t *testing.T) {
	t.Parallel()
	if postgresPool(nil) != nil {
		t.Fatal("nil infra must not yield a pool")
	}
	if postgresPool(&youtubeProducerInfrastructure{}) != nil {
		t.Fatal("missing postgres service must not yield a pool")
	}
}

func TestYouTubeProducerRuntimeStartsWithoutCommunityObservation(t *testing.T) {
	runtime := &YouTubeProducerRuntime{
		Logger:  testLogger(),
		Managed: lifecycle.NewManaged(func() {}),
	}
	runCtx, cancel := context.WithCancel(context.Background())
	runtime.startBackgroundServices(runCtx, make(chan error, 1))
	cancel()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := runtime.shutdownRuntime(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestProducerRuntimeHasNoLiveCommunityConsumeImport(t *testing.T) {
	t.Parallel()
	root := "."
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, filepath.Join(root, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if strings.Contains(path, "community_observation_consumer") {
				t.Fatalf("%s imports %s", name, path)
			}
		}
		src, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(set, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if ok && ident.Name == "CommunityObservationConsumer" {
				t.Fatalf("%s still names CommunityObservationConsumer", name)
			}
			return true
		})
	}
}
