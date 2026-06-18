package dbx

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLegacyORMRemovalSurfaceDoesNotRegress(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	legacyTagPrefix := "go" + "rm" + `:"`
	legacyIdentifier := "Go" + "rm"
	legacyAcronym := "GO" + "RM"
	legacySchemaCall := "Auto" + "Migrate("
	var offenders []string
	rootFS := os.DirFS(root)
	err := fs.WalkDir(rootFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		content, err := fs.ReadFile(rootFS, path)
		if err != nil {
			return err
		}
		text := string(content)
		if strings.Contains(text, legacyTagPrefix) ||
			strings.Contains(text, legacyIdentifier) ||
			strings.Contains(text, legacyAcronym) ||
			strings.Contains(text, legacySchemaCall) {
			offenders = append(offenders, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Go files: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("legacy ORM removal surface still present in %d files:\n%s", len(offenders), strings.Join(offenders, "\n"))
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	return root
}
