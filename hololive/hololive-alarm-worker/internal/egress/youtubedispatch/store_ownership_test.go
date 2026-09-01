package youtubedispatch

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestLifecycleStoreIsAlarmWorkerInternal(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	hololiveRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	legacyDir := filepath.Join(hololiveRoot, "hololive-shared", "pkg", "service", "youtube", "outbox", "store")
	if _, err := os.Stat(legacyDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("legacy shared store must not exist: err=%v", err)
	}

	hits, productionImporters, err := findLifecycleStoreOwnershipViolations(hololiveRoot)
	if err != nil {
		t.Fatalf("scan lifecycle store imports: %v", err)
	}

	if len(hits) > 0 {
		slices.Sort(hits)
		t.Fatalf("lifecycle store ownership violations:\n%s", strings.Join(hits, "\n"))
	}

	if productionImporters == 0 {
		t.Fatal("alarm-worker has no production lifecycle store importer")
	}
}

func findLifecycleStoreOwnershipViolations(hololiveRoot string) ([]string, int, error) {
	const (
		legacyImport = "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/store"
		workerImport = "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	)

	fset := token.NewFileSet()
	hits := make([]string, 0)
	productionImporters := 0

	err := filepath.WalkDir(hololiveRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "target", "vendor":
				return filepath.SkipDir
			}

			return nil
		}

		if filepath.Ext(path) != ".go" {
			return nil
		}

		relPath, err := filepath.Rel(hololiveRoot, path)
		if err != nil {
			return fmt.Errorf("relative path: %w", err)
		}

		fileNode, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", relPath, err)
		}

		for _, spec := range fileNode.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			switch importPath {
			case legacyImport:
				hits = append(hits, relPath+": legacy shared store import")
			case workerImport:
				if !strings.HasPrefix(relPath, "hololive-alarm-worker"+string(filepath.Separator)) {
					hits = append(hits, relPath+": worker-internal store imported outside alarm-worker")
					continue
				}

				if !strings.HasSuffix(relPath, "_test.go") {
					productionImporters++
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("walk hololive modules: %w", err)
	}

	return hits, productionImporters, nil
}
