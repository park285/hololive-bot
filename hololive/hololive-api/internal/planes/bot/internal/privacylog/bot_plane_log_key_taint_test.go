package privacylog

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

var keyTaintSanitizers = map[string]struct{}{
	"Pseudonym": {}, "IdentifierToken": {}, "RoomIDAttr": {}, "ChatIDAttr": {}, "RoomAttr": {}, "ChatAttr": {},
}

type keyTaint struct {
	builders     map[string]struct{}
	taintedParam map[string]map[int]struct{}
}

func TestIdentifierBearingKeysNeverReachLogAttrValues(t *testing.T) {
	t.Parallel()

	fileSet := token.NewFileSet()
	sources := parseScannedRoots(t, fileSet)
	taint := analyzeKeyTaint(sources)

	for _, seed := range []string{"BuildNotifyClaimKey", "BuildUpcomingEventKey", "BuildRoomAlarmKey"} {
		if _, ok := taint.builders[seed]; !ok {
			t.Fatalf("%q was not recognised as an identifier-bearing key builder; "+
				"the analysis is not reading the keys package", seed)
		}
	}

	for _, report := range taint.violations(fileSet, sources) {
		t.Error(report)
	}
}

func TestKeyTaintAnalysisFlagsBuilderResultsInLogValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		source  string
		flagged bool
	}{
		{
			name:    "builder result through a local variable",
			source:  `package p; func b(roomID string) string { return roomID }; func f(){ k := b(r); logger.Warn("m", slog.String("key", k)) }`,
			flagged: true,
		},
		{
			name:    "builder result inlined into the attr value",
			source:  `package p; func b(roomID string) string { return roomID }; func f(){ logger.Warn("m", slog.String("key", b(r))) }`,
			flagged: true,
		},
		{
			name:    "builder result forwarded into a callee parameter",
			source:  `package p; func b(roomID string) string { return roomID }; func g(key string){ logger.Warn("m", slog.String("key", key)) }; func f(){ g(b(r)) }`,
			flagged: true,
		},
		{
			name:    "builder result on a loose key/value pair",
			source:  `package p; func b(roomID string) string { return roomID }; func f(){ k := b(r); logger.Warn("m", "key", k) }`,
			flagged: true,
		},
		{
			name:    "privacylog helper launders the builder result",
			source:  `package p; func b(roomID string) string { return roomID }; func f(){ k := b(r); logger.Warn("m", slog.String("key_token", privacylog.Pseudonym(k))) }`,
			flagged: false,
		},
		{
			name:    "unrelated value is untouched",
			source:  `package p; func b(roomID string) string { return roomID }; func f(){ logger.Warn("m", slog.String("channel_id", channelID)) }`,
			flagged: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, "inject.go", tc.source, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse injected source: %v", err)
			}

			sources := map[string]*ast.File{"inject.go": file}
			reports := analyzeKeyTaint(sources).violations(fileSet, sources)
			if flagged := len(reports) > 0; flagged != tc.flagged {
				t.Fatalf("flagged=%v, want %v (reports=%v) for %s", flagged, tc.flagged, reports, tc.source)
			}
		})
	}
}

func analyzeKeyTaint(sources map[string]*ast.File) keyTaint {
	taint := keyTaint{
		builders:     map[string]struct{}{},
		taintedParam: map[string]map[int]struct{}{},
	}
	for _, file := range sources {
		for _, fn := range functionDecls(file) {
			if returnsOneString(fn) && hasRoomIdentifierParam(fn) {
				taint.builders[fn.Name.Name] = struct{}{}
			}
		}
	}

	for range len(sources) + 4 {
		if !taint.propagate(sources) {
			break
		}
	}

	return taint
}

func (k keyTaint) propagate(sources map[string]*ast.File) bool {
	changed := false
	for path, file := range sources {
		scope := filepath.Dir(path)
		for _, fn := range functionDecls(file) {
			if k.propagateThrough(scope, fn) {
				changed = true
			}
		}
	}

	return changed
}

func (k keyTaint) propagateThrough(scope string, fn *ast.FuncDecl) bool {
	tainted := k.taintedLocals(scope, fn)

	changed := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.ReturnStmt:
			if k.anyTainted(typed.Results, tainted) {
				changed = k.markBuilder(fn.Name.Name) || changed
			}
		case *ast.CallExpr:
			changed = k.propagateIntoCallee(scope, typed, tainted) || changed
		}

		return true
	})

	return changed
}

func (k keyTaint) propagateIntoCallee(scope string, call *ast.CallExpr, tainted map[string]struct{}) bool {
	changed := false
	for index, arg := range call.Args {
		if k.isTainted(arg, tainted) {
			changed = k.markParam(scope+"."+calleeName(call), index) || changed
		}
	}

	return changed
}

func (k keyTaint) anyTainted(exprs []ast.Expr, tainted map[string]struct{}) bool {
	for _, expr := range exprs {
		if k.isTainted(expr, tainted) {
			return true
		}
	}

	return false
}

func (k keyTaint) taintedLocals(scope string, fn *ast.FuncDecl) map[string]struct{} {
	tainted := map[string]struct{}{}
	for index, name := range flattenParams(fn) {
		if _, ok := k.taintedParam[scope+"."+fn.Name.Name][index]; ok && name != "" && name != "_" {
			tainted[name] = struct{}{}
		}
	}

	for k.growTaintedLocals(fn, tainted) {
		continue
	}

	return tainted
}

func (k keyTaint) growTaintedLocals(fn *ast.FuncDecl, tainted map[string]struct{}) bool {
	grew := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if k.anyTainted(assign.Rhs, tainted) {
			grew = markTaintedNames(assign.Lhs, tainted) || grew
		}

		return true
	})

	return grew
}

func markTaintedNames(exprs []ast.Expr, tainted map[string]struct{}) bool {
	grew := false
	for _, expr := range exprs {
		name, ok := expr.(*ast.Ident)
		if !ok || name.Name == "_" {
			continue
		}
		if _, seen := tainted[name.Name]; !seen {
			tainted[name.Name] = struct{}{}
			grew = true
		}
	}

	return grew
}

func (k keyTaint) isTainted(expr ast.Expr, tainted map[string]struct{}) bool {
	switch typed := expr.(type) {
	case *ast.Ident:
		_, ok := tainted[typed.Name]

		return ok
	case *ast.CallExpr:
		name := calleeName(typed)
		if _, clean := keyTaintSanitizers[name]; clean {
			return false
		}
		if _, builder := k.builders[name]; builder {
			return true
		}
		if name == "string" && len(typed.Args) == 1 {
			return k.isTainted(typed.Args[0], tainted)
		}

		return false
	case *ast.ParenExpr:
		return k.isTainted(typed.X, tainted)
	case *ast.BinaryExpr:
		return k.isTainted(typed.X, tainted) || k.isTainted(typed.Y, tainted)
	case *ast.SliceExpr:
		return k.isTainted(typed.X, tainted)
	case *ast.IndexExpr:
		return k.isTainted(typed.X, tainted)
	case *ast.UnaryExpr:
		return k.isTainted(typed.X, tainted)
	}

	return false
}

func (k keyTaint) violations(fileSet *token.FileSet, sources map[string]*ast.File) []string {
	var reports []string
	for path, file := range sources {
		scope := filepath.Dir(path)
		for _, fn := range functionDecls(file) {
			tainted := k.taintedLocals(scope, fn)

			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, value := range logAttrValues(call) {
					if !k.isTainted(value, tainted) {
						continue
					}
					reports = append(reports, fmt.Sprintf(
						"%s: this log attr value carries a cache key built from a room identifier; "+
							"log the non-identifying parts as their own attrs and the room through privacylog.RoomIDAttr",
						fileSet.Position(value.Pos())))
				}

				return true
			})
		}
	}

	return reports
}

func logAttrValues(call *ast.CallExpr) []ast.Expr {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	receiver, hasReceiver := selector.X.(*ast.Ident)
	if hasReceiver && receiver.Name == "slog" {
		if _, isAttr := slogAttrConstructors[selector.Sel.Name]; isAttr && len(call.Args) > 1 {
			return call.Args[1:]
		}

		return nil
	}
	if hasReceiver {
		if _, facade := structuredLogFacades[receiver.Name]; facade {
			return nil
		}
	}
	if _, isLevel := slogLevelMethods[selector.Sel.Name]; !isLevel {
		return nil
	}

	var values []ast.Expr
	for _, index := range looseKeyIndexes(call, selector.Sel.Name) {
		if index+1 < len(call.Args) {
			values = append(values, call.Args[index+1])
		}
	}

	return values
}

func (k keyTaint) markBuilder(name string) bool {
	if _, ok := k.builders[name]; ok {
		return false
	}
	k.builders[name] = struct{}{}

	return true
}

func (k keyTaint) markParam(name string, index int) bool {
	if name == "" {
		return false
	}
	if _, ok := k.taintedParam[name]; !ok {
		k.taintedParam[name] = map[int]struct{}{}
	}
	if _, ok := k.taintedParam[name][index]; ok {
		return false
	}
	k.taintedParam[name][index] = struct{}{}

	return true
}

func functionDecls(file *ast.File) []*ast.FuncDecl {
	var decls []*ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Body != nil {
			decls = append(decls, fn)
		}
	}

	return decls
}

func flattenParams(fn *ast.FuncDecl) []string {
	if fn.Type.Params == nil {
		return nil
	}

	var names []string
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			names = append(names, "")

			continue
		}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}

	return names
}

func returnsOneString(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	if len(fn.Type.Results.List[0].Names) > 1 {
		return false
	}
	identifier, ok := fn.Type.Results.List[0].Type.(*ast.Ident)

	return ok && identifier.Name == "string"
}

func hasRoomIdentifierParam(fn *ast.FuncDecl) bool {
	for _, name := range flattenParams(fn) {
		if name == "roomID" {
			return true
		}
	}

	return false
}

func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}

	return ""
}
