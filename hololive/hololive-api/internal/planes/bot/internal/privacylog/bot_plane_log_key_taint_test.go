package privacylog

import (
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
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
		if !hasCallableNamed(taint.builders, seed) {
			t.Fatalf("%q was not recognized as an identifier-bearing key builder; "+
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

			sources := untypedTestScannedSources(fileSet, map[string]*ast.File{"inject.go": file})
			reports := analyzeKeyTaint(sources).violations(fileSet, sources)

			if flagged := len(reports) > 0; flagged != tc.flagged {
				t.Fatalf("flagged=%v, want %v (reports=%v) for %s", flagged, tc.flagged, reports, tc.source)
			}
		})
	}
}

func TestKeyTaintAnalysisUsesReceiverQualifiedCallables(t *testing.T) {
	t.Parallel()

	t.Run("propagates into a different package", func(t *testing.T) {
		t.Parallel()

		fileSet := token.NewFileSet()
		sources := parseTaintFixture(t, fileSet, map[string]string{
			"example/cache/cache.go": `package cache; type Service struct{}; func (*Service) Build(roomID string) string { return roomID }`,
			"example/sink/sink.go":   `package sink; import "example/cache"; type Logger struct{}; func (Logger) Warn(string, ...any) {}; var logger Logger; type Service struct{}; func (*Service) emit(key string) { logger.Warn("m", "cache_key", key) }; func (s *Service) Run(room string) { c := &cache.Service{}; s.emit(c.Build(room)) }`,
		})
		taint := analyzeKeyTaint(sources)

		if reports := taint.violations(fileSet, sources); len(reports) != 1 {
			t.Fatalf("cross-package reports = %v, want one (builders=%v params=%v)", reports, taint.builders, taint.taintedParam)
		}
	})

	t.Run("does not merge same-named methods across packages", func(t *testing.T) {
		t.Parallel()

		fileSet := token.NewFileSet()
		sources := parseTaintFixture(t, fileSet, map[string]string{
			"example/sensitive/key.go": `package sensitive; type Service struct{}; func (*Service) Build(roomID string) string { return roomID }`,
			"example/clean/use.go":     `package clean; type Logger struct{}; func (Logger) Warn(string, ...any) {}; var logger Logger; type Service struct{}; func (*Service) Build(channelID string) string { return channelID }; func (s *Service) Run(channel string) { logger.Warn("m", "cache_key", s.Build(channel)) }`,
		})

		if reports := analyzeKeyTaint(sources).violations(fileSet, sources); len(reports) != 0 {
			t.Fatalf("same-named clean function produced reports: %v", reports)
		}
	})

	t.Run("does not merge same-named methods on different receivers", func(t *testing.T) {
		t.Parallel()

		fileSet := token.NewFileSet()
		sources := parseTaintFixture(t, fileSet, map[string]string{
			"example/service/use.go": `package service; type Logger struct{}; func (Logger) Warn(string, ...any) {}; var logger Logger; type Sensitive struct{}; func (*Sensitive) Build(roomID string) string { return roomID }; type Clean struct{}; func (*Clean) Build(channelID string) string { return channelID }; func (c *Clean) Run(channel string) { logger.Warn("m", "cache_key", c.Build(channel)) }`,
		})

		if reports := analyzeKeyTaint(sources).violations(fileSet, sources); len(reports) != 0 {
			t.Fatalf("same-package receiver methods produced reports: %v", reports)
		}
	})
}

func parseTaintFixture(t *testing.T, fileSet *token.FileSet, contents map[string]string) scannedSources {
	t.Helper()

	files := make(map[string]*ast.File, len(contents))
	for filename, source := range contents {
		file, err := parser.ParseFile(fileSet, filename, source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}

		files[filename] = file
	}

	return typedTestScannedSources(t, fileSet, files)
}

func untypedTestScannedSources(fileSet *token.FileSet, files map[string]*ast.File) scannedSources {
	return scannedSources{files: files, importPathByScope: fixtureImportPaths(files), fileSet: fileSet}
}

func typedTestScannedSources(t *testing.T, fileSet *token.FileSet, files map[string]*ast.File) scannedSources {
	t.Helper()

	sources := scannedSources{files: files, importPathByScope: fixtureImportPaths(files), fileSet: fileSet, sourceImports: true}
	if err := sources.loadTypes(); err != nil {
		t.Fatalf("type-check taint fixture: %v", err)
	}

	return sources
}

func fixtureImportPaths(files map[string]*ast.File) map[string]string {
	importPaths := make(map[string]string)

	for filename := range files {
		scope := filepath.Clean(filepath.Dir(filename))

		importPaths[scope] = filepath.ToSlash(scope)
	}

	return importPaths
}

func hasCallableNamed(callables map[string]struct{}, name string) bool {
	for callable := range callables {
		if strings.HasSuffix(callable, "."+name) {
			return true
		}
	}

	return false
}

func analyzeKeyTaint(sources scannedSources) keyTaint {
	taint := keyTaint{
		builders:     map[string]struct{}{},
		taintedParam: map[string]map[int]struct{}{},
	}

	for filename, file := range sources.files {
		for _, fn := range functionDecls(file) {
			if returnsOneString(fn) && hasRoomIdentifierParam(fn) {
				taint.builders[sources.functionID(filename, fn)] = struct{}{}
			}
		}
	}

	for range len(sources.files) + 4 {
		if !taint.propagate(sources) {
			break
		}
	}

	return taint
}

func (k keyTaint) propagate(sources scannedSources) bool {
	changed := false

	for filename, file := range sources.files {
		for _, fn := range functionDecls(file) {
			if k.propagateThrough(sources, filename, file, fn) {
				changed = true
			}
		}
	}

	return changed
}

func (k keyTaint) propagateThrough(sources scannedSources, filename string, file *ast.File, fn *ast.FuncDecl) bool {
	tainted := k.taintedLocals(sources, filename, file, fn)

	changed := false

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.ReturnStmt:
			if k.anyTainted(sources, filename, file, typed.Results, tainted) {
				changed = k.markBuilder(sources.functionID(filename, fn)) || changed
			}
		case *ast.CallExpr:
			changed = k.propagateIntoCallee(sources, filename, file, typed, tainted) || changed
		}

		return true
	})

	return changed
}

func (k keyTaint) propagateIntoCallee(sources scannedSources, filename string, file *ast.File, call *ast.CallExpr, tainted map[string]struct{}) bool {
	changed := false

	for index, arg := range call.Args {
		if k.isTainted(sources, filename, file, arg, tainted) {
			changed = k.markParam(sources.calleeID(filename, file, call), index) || changed
		}
	}

	return changed
}

func (k keyTaint) anyTainted(sources scannedSources, filename string, file *ast.File, exprs []ast.Expr, tainted map[string]struct{}) bool {
	for _, expr := range exprs {
		if k.isTainted(sources, filename, file, expr, tainted) {
			return true
		}
	}

	return false
}

func (k keyTaint) taintedLocals(sources scannedSources, filename string, file *ast.File, fn *ast.FuncDecl) map[string]struct{} {
	tainted := map[string]struct{}{}

	for index, name := range flattenParams(fn) {
		if _, ok := k.taintedParam[sources.functionID(filename, fn)][index]; ok && name != "" && name != "_" {
			tainted[name] = struct{}{}
		}
	}

	for k.growTaintedLocals(sources, filename, file, fn, tainted) {
		continue
	}

	return tainted
}

func (k keyTaint) growTaintedLocals(sources scannedSources, filename string, file *ast.File, fn *ast.FuncDecl, tainted map[string]struct{}) bool {
	grew := false

	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}

		if k.anyTainted(sources, filename, file, assign.Rhs, tainted) {
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

func (k keyTaint) isTainted(sources scannedSources, filename string, file *ast.File, expr ast.Expr, tainted map[string]struct{}) bool {
	switch typed := expr.(type) {
	case *ast.Ident:
		_, ok := tainted[typed.Name]

		return ok
	case *ast.CallExpr:
		name := calleeName(typed)
		if _, clean := keyTaintSanitizers[name]; clean {
			return false
		}

		if _, builder := k.builders[sources.calleeID(filename, file, typed)]; builder {
			return true
		}

		if name == "string" && len(typed.Args) == 1 {
			return k.isTainted(sources, filename, file, typed.Args[0], tainted)
		}

		return false
	case *ast.ParenExpr:
		return k.isTainted(sources, filename, file, typed.X, tainted)
	case *ast.BinaryExpr:
		return k.isTainted(sources, filename, file, typed.X, tainted) ||
			k.isTainted(sources, filename, file, typed.Y, tainted)
	case *ast.SliceExpr:
		return k.isTainted(sources, filename, file, typed.X, tainted)
	case *ast.IndexExpr:
		return k.isTainted(sources, filename, file, typed.X, tainted)
	case *ast.UnaryExpr:
		return k.isTainted(sources, filename, file, typed.X, tainted)
	}

	return false
}

func (k keyTaint) violations(fileSet *token.FileSet, sources scannedSources) []string {
	var reports []string

	for filename, file := range sources.files {
		for _, fn := range functionDecls(file) {
			tainted := k.taintedLocals(sources, filename, file, fn)

			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}

				for _, value := range logAttrValues(call) {
					if !k.isTainted(sources, filename, file, value, tainted) {
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

func (s *scannedSources) functionID(filename string, fn *ast.FuncDecl) string {
	if info := s.typesInfo[s.files[filename]]; info != nil {
		if callable, ok := info.Defs[fn.Name].(*types.Func); ok {
			return callableID(callable)
		}
	}

	receiver := receiverTypeName(fn)
	if receiver != "" {
		return s.packageID(filename) + "." + receiver + "." + fn.Name.Name
	}

	return s.packageID(filename) + "." + fn.Name.Name
}

func (s *scannedSources) packageID(filename string) string {
	scope := filepath.Clean(filepath.Dir(filename))
	if importPath := s.importPathByScope[scope]; importPath != "" {
		return importPath
	}

	return filepath.ToSlash(scope)
}

func (s *scannedSources) calleeID(filename string, file *ast.File, call *ast.CallExpr) string {
	if callable := callableFromCall(s.typesInfo[file], call); callable != nil {
		return callableID(callable)
	}

	if fun, ok := call.Fun.(*ast.Ident); ok {
		return s.packageID(filename) + "." + fun.Name
	}

	return ""
}

func callableFromCall(info *types.Info, call *ast.CallExpr) *types.Func {
	if info == nil {
		return nil
	}

	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return objectFunc(info.Uses[fun])
	case *ast.SelectorExpr:
		if selection := info.Selections[fun]; selection != nil {
			return objectFunc(selection.Obj())
		}

		return objectFunc(info.Uses[fun.Sel])
	default:
		return nil
	}
}

func objectFunc(object types.Object) *types.Func {
	callable, ok := object.(*types.Func)
	if !ok {
		return nil
	}

	return callable
}

func callableID(fn *types.Func) string {
	pkg := fn.Pkg()
	if pkg == nil {
		return fn.FullName()
	}

	signature, ok := fn.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return pkg.Path() + "." + fn.Name()
	}

	receiver := types.TypeString(signature.Recv().Type(), func(other *types.Package) string {
		if other == pkg {
			return ""
		}

		return other.Path()
	})

	return pkg.Path() + "." + receiver + "." + fn.Name()
}

func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}

	expr := fn.Recv.List[0].Type
	if pointer, ok := expr.(*ast.StarExpr); ok {
		expr = pointer.X
	}

	if identifier, ok := expr.(*ast.Ident); ok {
		return identifier.Name
	}

	return ""
}

type sourceTypesImporter struct {
	sources  *scannedSources
	fallback types.Importer
	packages map[string]*types.Package
	checking map[string]bool
}

func (s *scannedSources) loadTypes() error {
	s.typesInfo = make(map[*ast.File]*types.Info, len(s.files))

	var exportRoot *os.Root

	if len(s.exports) > 0 {
		var err error

		exportRoot, err = os.OpenRoot(s.buildCacheRoot)
		if err != nil {
			return fmt.Errorf("open Go build cache root: %w", err)
		}
	}

	lookup := func(importPath string) (io.ReadCloser, error) {
		export := s.exports[importPath]
		if export == "" {
			return nil, fmt.Errorf("no export data for %s", importPath)
		}

		return openExportFile(exportRoot, s.buildCacheRoot, export)
	}
	loader := &sourceTypesImporter{
		sources:  s,
		fallback: importer.ForCompiler(s.fileSet, "gc", lookup),
		packages: map[string]*types.Package{},
		checking: map[string]bool{},
	}
	paths := make([]string, 0, len(s.importPathByScope))

	for _, importPath := range s.importPathByScope {
		paths = append(paths, importPath)
	}

	sort.Strings(paths)

	var loadErr error

	for _, importPath := range paths {
		if _, err := loader.loadSourcePackage(importPath); err != nil {
			loadErr = err
			break
		}
	}

	if exportRoot != nil {
		loadErr = errors.Join(loadErr, exportRoot.Close())
	}

	return loadErr
}

func openExportFile(root *os.Root, rootPath, exportPath string) (io.ReadCloser, error) {
	if root == nil {
		return nil, errors.New("Go build cache root is unavailable")
	}

	relative, err := filepath.Rel(rootPath, filepath.Clean(exportPath))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("export data path %q is outside Go build cache %q", exportPath, rootPath)
	}

	file, err := root.Open(relative)
	if err != nil {
		return nil, fmt.Errorf("open export data for %q: %w", exportPath, err)
	}

	info, err := file.Stat()
	if err != nil {
		closeErr := file.Close()

		return nil, fmt.Errorf("stat export data for %q: %w", exportPath, errors.Join(err, closeErr))
	}

	if !info.Mode().IsRegular() {
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close non-regular export data path %q: %w", exportPath, err)
		}

		return nil, fmt.Errorf("export data path %q is not a regular file", exportPath)
	}

	return file, nil
}

func (i *sourceTypesImporter) Import(importPath string) (*types.Package, error) {
	if !i.sources.sourceImports {
		out, err := i.fallback.Import(importPath)
		if err != nil {
			return nil, fmt.Errorf("import: %w", err)
		}

		return out, nil
	}

	out, err := i.loadSourcePackage(importPath)
	if err != nil {
		//nolint:nilnil // types.Config.Check는 오류와 함께 부분 완성된 패키지를 돌려주고 go/types가 이를 사용하므로, 오류 경로에서도 그대로 전달해야 한다.
		return out, fmt.Errorf("load source package: %w", err)
	}

	return out, nil
}

func (i *sourceTypesImporter) loadSourcePackage(importPath string) (*types.Package, error) {
	if pkg := i.packages[importPath]; pkg != nil {
		return pkg, nil
	}

	scope := ""

	for candidateScope, candidatePath := range i.sources.importPathByScope {
		if candidatePath == importPath {
			scope = candidateScope
			break
		}
	}

	if scope == "" {
		out, err := i.fallback.Import(importPath)
		if err != nil {
			return nil, fmt.Errorf("import: %w", err)
		}

		return out, nil
	}

	if i.checking[importPath] {
		return nil, fmt.Errorf("type import cycle involving %s", importPath)
	}

	i.checking[importPath] = true

	defer delete(i.checking, importPath)

	var files []*ast.File

	for filename, file := range i.sources.files {
		if filepath.Clean(filepath.Dir(filename)) == scope {
			files = append(files, file)
		}
	}

	info := &types.Info{
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Types:      map[ast.Expr]types.TypeAndValue{},
	}
	config := types.Config{Importer: i, Sizes: types.SizesFor("gc", runtime.GOARCH)}
	pkg, err := config.Check(importPath, i.sources.fileSet, files, info)

	if pkg != nil {
		i.packages[importPath] = pkg

		for _, file := range files {
			i.sources.typesInfo[file] = info
		}
	}

	if err != nil {
		//nolint:nilnil // types.Config.Check는 오류와 함께 부분 완성된 패키지를 돌려주고 go/types가 이를 사용하므로, 오류 경로에서도 그대로 전달해야 한다.
		return pkg, fmt.Errorf("type-check %s: %w", importPath, err)
	}

	return pkg, nil
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
	return slices.Contains(flattenParams(fn), "roomID")
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
