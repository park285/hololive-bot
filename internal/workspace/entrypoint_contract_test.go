package workspace

import (
	jsonv2 "encoding/json/v2"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type entrypointContract struct {
	Path        string   `json:"path"`
	MustContain []string `json:"must_contain"`
}

func TestCommandEntrypointsStayAnchoredToOwningHelpers(t *testing.T) {
	t.Parallel()

	root := repoRootFromHelper(t)
	contracts := loadEntrypointContracts(t, root)

	if len(contracts) == 0 {
		t.Fatal("entrypoint contract manifest 가 비어 있습니다")
	}

	for _, contract := range contracts {
		t.Run(contract.Path, func(t *testing.T) {
			t.Parallel()

			content := []byte(readRepoFile(t, root, contract.Path))

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

	root := repoRootFromHelper(t)
	contracts := loadEntrypointContracts(t, root)
	manifestPaths := make([]string, 0, len(contracts))

	for _, contract := range contracts {
		manifestPaths = append(manifestPaths, filepath.ToSlash(contract.Path))
	}

	slices.Sort(manifestPaths)

	discoveredPaths := make([]string, 0, len(manifestPaths))

	if err := fs.WalkDir(os.DirFS(filepath.Join(root, "hololive")), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if filepath.Base(path) != "main.go" {
			return nil
		}

		slashed := filepath.ToSlash(filepath.Join("hololive", path))
		if !strings.Contains(slashed, "/cmd/") {
			return nil
		}

		discoveredPaths = append(discoveredPaths, slashed)

		return nil
	}); err != nil {
		t.Fatalf("command entrypoint scan 실패: %v", err)
	}

	slices.Sort(discoveredPaths)

	if len(manifestPaths) != len(discoveredPaths) {
		t.Fatalf("manifest count=%d discovered count=%d\nmanifest=%v\ndiscovered=%v", len(manifestPaths), len(discoveredPaths), manifestPaths, discoveredPaths)
	}

	for i := range discoveredPaths {
		if manifestPaths[i] != discoveredPaths[i] {
			t.Fatalf("entrypoint manifest mismatch at %d: manifest=%q discovered=%q", i, manifestPaths[i], discoveredPaths[i])
		}
	}
}

func loadEntrypointContracts(t *testing.T, root string) []entrypointContract {
	t.Helper()

	data := []byte(readRepoFile(t, root, "internal/workspace/testdata/entrypoint_contracts.json"))

	var contracts []entrypointContract

	if err := jsonv2.Unmarshal(data, &contracts); err != nil {
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
	case *ast.CompositeLit:
		return renderCallPath(node.Type)
	case *ast.IndexExpr:
		return renderCallPath(node.X)
	case *ast.IndexListExpr:
		return renderCallPath(node.X)
	default:
		return ""
	}
}
