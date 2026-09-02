package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRootFromHelper(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve workspace helper path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func repoFS(root string) fs.FS {
	return os.DirFS(root)
}

func cleanRepoRel(t *testing.T, rel string) string {
	t.Helper()

	cleaned := filepath.ToSlash(filepath.Clean(rel))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(cleaned) {
		t.Fatalf("invalid repo-relative path %q", rel)
	}

	return cleaned
}

func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()

	data, err := fs.ReadFile(repoFS(root), cleanRepoRel(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}

	return string(data)
}
