package member

import (
	"go/ast"
	"go/parser"
	"go/token"
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

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
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

func queryTextFromFunc(t *testing.T, fn *ast.FuncDecl) string {
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

		switch rhs := assign.Rhs[0].(type) {
		case *ast.BasicLit:
			if rhs.Kind != token.STRING {
				return true
			}

			unquoted, err := strconv.Unquote(rhs.Value)
			if err != nil {
				t.Fatalf("unquote query literal: %v", err)
			}
			query = strings.ToLower(unquoted)
		case *ast.CallExpr:
			query = strings.ToLower(queryTextFromMustSQLCall(t, rhs))
		default:
			return true
		}
		return false
	})

	if query == "" {
		t.Fatal("query text not found")
	}

	return query
}

func queryTextFromMustSQLCall(t *testing.T, call *ast.CallExpr) string {
	t.Helper()

	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != "mustSQL" || len(call.Args) != 1 {
		return ""
	}

	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}

	name, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatalf("unquote query asset name: %v", err)
	}
	return mustSQL(name)
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
			recordMemberCompositeFields(fields, node)
		case *ast.AssignStmt:
			recordMemberAssignmentFields(fields, node)
		}
		return true
	})

	return fields
}

func recordMemberCompositeFields(fields map[string]bool, node *ast.CompositeLit) {
	sel, ok := node.Type.(*ast.SelectorExpr)
	if !ok {
		return
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "domain" || sel.Sel.Name != "Member" {
		return
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
}

func recordMemberAssignmentFields(fields map[string]bool, node *ast.AssignStmt) {
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

func TestRepositorySource_GetAllMembersQuerySelectsPhoto(t *testing.T) {
	t.Parallel()

	file := loadRepositoryAST(t)
	query := queryTextFromFunc(t, findRepositoryFunc(t, file, "GetAllMembers"))

	if !strings.Contains(query, "photo") {
		t.Fatalf("GetAllMembers query must select photo column, query=%q", query)
	}
}

func TestRepositorySource_MemberQueriesSelectShortKoreanName(t *testing.T) {
	t.Parallel()

	file := loadRepositoryAST(t)
	for _, funcName := range []string{
		"FindByChannelID",
		"FindByName",
		"FindByAlias",
		"GetAllMembers",
		"GetMembersWithPhoto",
		"GetMemberWithPhotoByChannelID",
		"FindAllByName",
		"FindByNameAndOrg",
	} {
		query := queryTextFromFunc(t, findRepositoryFunc(t, file, funcName))
		if !strings.Contains(query, "short_korean_name") {
			t.Fatalf("%s query must select short_korean_name column, query=%q", funcName, query)
		}
	}
}

func TestRepositorySource_ScanMemberCarriesMetadataAndPhoto(t *testing.T) {
	t.Parallel()

	file := loadRepositoryAST(t)
	fn := findRepositoryFunc(t, file, "scanMember")

	params := paramNames(fn)
	for _, want := range []string{"org", "suborg", "syncSource", "photo", "shortKoreanName"} {
		if !slices.Contains(params, want) {
			t.Fatalf("scanMember must accept %s parameter, params=%v", want, params)
		}
	}

	args := scanMemberForwardedArgs(t, fn)
	for _, want := range []string{"photo", "org", "suborg", "syncSource", "shortKoreanName"} {
		if !slices.Contains(args, want) {
			t.Fatalf("scanMember must forward %s to scanMemberWithPhoto, args=%v", want, args)
		}
	}
}

func TestRepositorySource_ScanMemberWithPhotoSetsMetadataFields(t *testing.T) {
	t.Parallel()

	file := loadRepositoryAST(t)
	fields := assignedMemberFields(findRepositoryFunc(t, file, "scanMemberWithPhoto"))

	for _, want := range []string{"Org", "Suborg", "SyncSource", "ShortKoreanName"} {
		if !fields[want] {
			t.Fatalf("scanMemberWithPhoto must assign %s field, assigned=%v", want, fields)
		}
	}
}
