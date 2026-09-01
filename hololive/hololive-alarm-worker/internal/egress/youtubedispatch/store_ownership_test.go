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

const (
	legacyLifecycleStoreImport = "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/store"
	workerLifecycleStoreImport = "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
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
	scan := lifecycleStoreOwnershipScan{
		hololiveRoot: hololiveRoot,
		fileSet:      token.NewFileSet(),
		hits:         make([]string, 0),
	}

	err := filepath.WalkDir(hololiveRoot, scan.visit)
	if err != nil {
		return nil, 0, fmt.Errorf("walk hololive modules: %w", err)
	}

	return scan.hits, scan.productionImporters, nil
}

type lifecycleStoreOwnershipScan struct {
	hololiveRoot        string
	fileSet             *token.FileSet
	hits                []string
	productionImporters int
}

func (s *lifecycleStoreOwnershipScan) visit(path string, entry fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return fmt.Errorf("visit lifecycle store source: %w", walkErr)
	}

	if entry.IsDir() {
		switch entry.Name() {
		case ".git", "target", "vendor":
			return filepath.SkipDir
		default:
			return nil
		}
	}

	if filepath.Ext(path) != ".go" {
		return nil
	}

	if err := s.inspectGoFile(path); err != nil {
		return fmt.Errorf("inspect lifecycle store source: %w", err)
	}

	return nil
}

func (s *lifecycleStoreOwnershipScan) inspectGoFile(path string) error {
	relPath, err := filepath.Rel(s.hololiveRoot, path)
	if err != nil {
		return fmt.Errorf("relative path: %w", err)
	}

	fileNode, err := parser.ParseFile(s.fileSet, path, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse %s: %w", relPath, err)
	}

	for _, spec := range fileNode.Imports {
		s.inspectImport(relPath, strings.Trim(spec.Path.Value, "\""))
	}

	return nil
}

func (s *lifecycleStoreOwnershipScan) inspectImport(relPath, importPath string) {
	switch importPath {
	case legacyLifecycleStoreImport:
		s.hits = append(s.hits, relPath+": legacy shared store import")
	case workerLifecycleStoreImport:
		s.inspectWorkerStoreImport(relPath)
	}
}

func (s *lifecycleStoreOwnershipScan) inspectWorkerStoreImport(relPath string) {
	if !strings.HasPrefix(relPath, "hololive-alarm-worker"+string(filepath.Separator)) {
		s.hits = append(s.hits, relPath+": worker-internal store imported outside alarm-worker")

		return
	}

	if !strings.HasSuffix(relPath, "_test.go") {
		s.productionImporters++
	}
}
