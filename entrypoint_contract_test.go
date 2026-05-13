package workspace

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type entrypointContract struct {
	Path        string   `json:"path"`
	MustContain []string `json:"must_contain"`
}

func TestCommandEntrypointsStayAnchoredToOwningHelpers(t *testing.T) {
	t.Parallel()

	contracts := loadEntrypointContracts(t)
	if len(contracts) == 0 {
		t.Fatal("entrypoint contract manifest 가 비어 있습니다")
	}

	for _, contract := range contracts {
		t.Run(contract.Path, func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(contract.Path)
			if err != nil {
				t.Fatalf("%s 읽기 실패: %v", contract.Path, err)
			}

			for _, needle := range contract.MustContain {
				if !fileContainsCallPath(t, contract.Path, content, needle) {
					t.Fatalf("%s must contain call %q", contract.Path, needle)
				}
			}
		})
	}
}

func TestEntrypointContractManifestCoversAllCommandMainFiles(t *testing.T) {
	t.Parallel()

	contracts := loadEntrypointContracts(t)
	manifestPaths := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		manifestPaths = append(manifestPaths, filepath.ToSlash(contract.Path))
	}
	sort.Strings(manifestPaths)

	discoveredPaths := make([]string, 0, len(manifestPaths))
	if err := filepath.WalkDir("hololive", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "main.go" {
			return nil
		}
		slashed := filepath.ToSlash(path)
		if !strings.Contains(slashed, "/cmd/") {
			return nil
		}
		discoveredPaths = append(discoveredPaths, slashed)
		return nil
	}); err != nil {
		t.Fatalf("command entrypoint scan 실패: %v", err)
	}
	sort.Strings(discoveredPaths)

	if len(manifestPaths) != len(discoveredPaths) {
		t.Fatalf("manifest count=%d discovered count=%d\nmanifest=%v\ndiscovered=%v", len(manifestPaths), len(discoveredPaths), manifestPaths, discoveredPaths)
	}

	for i := range discoveredPaths {
		if manifestPaths[i] != discoveredPaths[i] {
			t.Fatalf("entrypoint manifest mismatch at %d: manifest=%q discovered=%q", i, manifestPaths[i], discoveredPaths[i])
		}
	}
}

func TestDocsUseConsolidatedStreamIngesterOpsCommand(t *testing.T) {
	t.Parallel()

	deprecatedCommandPaths := []string{
		"hololive/hololive-stream-ingester/cmd/youtube-",
		"hololive/hololive-stream-ingester/cmd/ops/youtube-community-alarm-sent-history",
		"hololive/hololive-stream-ingester/cmd/ops/youtube-shorts-alarm-sent-history",
		"hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts-",
	}

	if err := filepath.WalkDir(filepath.Join("docs", "current"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, deprecatedPath := range deprecatedCommandPaths {
			if strings.Contains(string(content), deprecatedPath) {
				t.Fatalf("%s contains deprecated command path %q", path, deprecatedPath)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("docs/current scan 실패: %v", err)
	}
}

func loadEntrypointContracts(t *testing.T) []entrypointContract {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "entrypoint_contracts.json"))
	if err != nil {
		t.Fatalf("entrypoint contract manifest 읽기 실패: %v", err)
	}

	var contracts []entrypointContract
	if err := json.Unmarshal(data, &contracts); err != nil {
		t.Fatalf("entrypoint contract manifest 파싱 실패: %v", err)
	}
	return contracts
}

func fileContainsCallPath(t *testing.T, path string, content []byte, want string) bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, content, 0)
	if err != nil {
		t.Fatalf("%s 파싱 실패: %v", path, err)
	}

	normalizedWant := normalizeCallPath(want)
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if normalizeCallPath(renderCallPath(call.Fun)) == normalizedWant {
			found = true
			return false
		}
		return true
	})
	return found
}

func normalizeCallPath(path string) string {
	return strings.TrimSpace(strings.TrimSuffix(path, "("))
}

func renderCallPath(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.SelectorExpr:
		left := renderCallPath(node.X)
		if left == "" {
			return node.Sel.Name
		}
		return left + "." + node.Sel.Name
	default:
		return ""
	}
}
