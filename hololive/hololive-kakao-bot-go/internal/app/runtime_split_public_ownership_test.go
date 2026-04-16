package app

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestNoLegacyRuntimeSplitInternalImportsRemain(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	legacyRoot := "github.com/kapu/hololive-kakao-bot-go/internal/"
	sharedServiceRoot := "github.com/kapu/hololive-shared/pkg/service/"
	forbiddenImports := map[string]string{
		legacyRoot + "service/acl":          sharedServiceRoot + "acl",
		legacyRoot + "service/activity":     sharedServiceRoot + "activity",
		legacyRoot + "service/chzzk":        sharedServiceRoot + "chzzk",
		legacyRoot + "service/twitch":       sharedServiceRoot + "twitch",
		legacyRoot + "service/notification": sharedServiceRoot + "notification",
		legacyRoot + "errors":               "github.com/kapu/hololive-shared/pkg/apperrors",
	}

	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	fset := token.NewFileSet()
	var hits []string
	walkErr := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			switch d.Name() {
			case ".git", ".omx", ".worktrees", "vendor":
				return filepath.SkipDir
			}

			return nil
		}

		if filepath.Ext(path) != ".go" {
			return nil
		}

		relPath, relErr := filepath.Rel(moduleRoot, path)
		if relErr != nil {
			return relErr
		}

		fileNode, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}

		for _, spec := range fileNode.Imports {
			importPath := strings.Trim(spec.Path.Value, "\"")
			replacement, blocked := forbiddenImports[importPath]
			if !blocked {
				continue
			}

			hits = append(hits, relPath+": "+importPath+" -> "+replacement)
		}

		return nil
	})
	if walkErr != nil {
		t.Fatalf("scan module imports: %v", walkErr)
	}

	if len(hits) == 0 {
		return
	}

	sort.Strings(hits)
	t.Fatalf("legacy runtime split imports remain:\n%s", strings.Join(hits, "\n"))
}
