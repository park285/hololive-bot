package member

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func loadRepositoryAST(t *testing.T) *ast.File {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(".", "repository*.go"))
	if err != nil {
		t.Fatalf("glob repository source: %v", err)
	}

	merged := &ast.File{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read repository source %s: %v", path, err)
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse repository source %s: %v", path, err)
		}
		merged.Decls = append(merged.Decls, file.Decls...)
	}

	return merged
}

func findRepositoryFunc(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}

	t.Fatalf("function %s not found", name)
	return nil
}

func queryLiteralFromFunc(t *testing.T, fn *ast.FuncDecl) string {
	t.Helper()

	var query string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}

		lhs, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || lhs.Name != "query" {
			return true
		}

		lit, ok := assign.Rhs[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}

		unquoted, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatalf("unquote query literal: %v", err)
		}

		query = strings.ToLower(unquoted)
		return false
	})

	if query == "" {
		t.Fatal("query literal not found")
	}

	return query
}

func paramNames(fn *ast.FuncDecl) []string {
	if fn.Type.Params == nil {
		return nil
	}

	names := make([]string, 0, len(fn.Type.Params.List))
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}

	return names
}

func scanMemberForwardedArgs(t *testing.T, fn *ast.FuncDecl) []string {
	t.Helper()

	var args []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "scanMemberWithPhoto" {
			return true
		}

		for _, arg := range call.Args {
			switch v := arg.(type) {
			case *ast.Ident:
				args = append(args, v.Name)
			default:
				args = append(args, "")
			}
		}
		return false
	})

	if len(args) == 0 {
		t.Fatal("scanMemberWithPhoto call not found")
	}

	return args
}

func assignedMemberFields(fn *ast.FuncDecl) map[string]bool {
	fields := make(map[string]bool)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			sel, ok := node.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "domain" || sel.Sel.Name != "Member" {
				return true
			}
			for _, elt := range node.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if ok {
					fields[key.Name] = true
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				target, ok := sel.X.(*ast.Ident)
				if ok && target.Name == "member" {
					fields[sel.Sel.Name] = true
				}
			}
		}
		return true
	})

	return fields
}

func TestRepositorySource_GetAllMembersQuerySelectsPhoto(t *testing.T) {
	t.Parallel()

	file := loadRepositoryAST(t)
	query := queryLiteralFromFunc(t, findRepositoryFunc(t, file, "GetAllMembers"))

	if !strings.Contains(query, "photo") {
		t.Fatalf("GetAllMembers query must select photo column, query=%q", query)
	}
}

func TestRepositorySource_ScanMemberCarriesMetadataAndPhoto(t *testing.T) {
	t.Parallel()

	file := loadRepositoryAST(t)
	fn := findRepositoryFunc(t, file, "scanMember")

	params := paramNames(fn)
	for _, want := range []string{"org", "suborg", "syncSource", "photo"} {
		if !slices.Contains(params, want) {
			t.Fatalf("scanMember must accept %s parameter, params=%v", want, params)
		}
	}

	args := scanMemberForwardedArgs(t, fn)
	for _, want := range []string{"photo", "org", "suborg", "syncSource"} {
		if !slices.Contains(args, want) {
			t.Fatalf("scanMember must forward %s to scanMemberWithPhoto, args=%v", want, args)
		}
	}
}

func TestRepositorySource_ScanMemberWithPhotoSetsMetadataFields(t *testing.T) {
	t.Parallel()

	file := loadRepositoryAST(t)
	fields := assignedMemberFields(findRepositoryFunc(t, file, "scanMemberWithPhoto"))

	for _, want := range []string{"Org", "Suborg", "SyncSource"} {
		if !fields[want] {
			t.Fatalf("scanMemberWithPhoto must assign %s field, assigned=%v", want, fields)
		}
	}
}
