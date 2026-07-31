package privacylog

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"log/slog"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// bot plane이 같은 사용자 액션 안에서 호출하는 alarm/notification 모듈까지 포함한다. 모듈 경계를
// 넘지만 로그 경계는 프로세스 단위라, plane 안에서만 닫으면 한 콜 뒤에서 평문이 그대로 나간다.
// cache/delivery/lease는 alarm이 부르는 Valkey 하부라, plain string 파라미터를 받는 순간 taint가
// 끊긴다. 값 추적 대신 "이 root 안에서는 key/field 리터럴 금지"라는 key 이름 규칙으로 닫는다.
var scannedRoots = []string{
	"../..",
	"../../../../../../hololive-shared/pkg/service/alarm",
	"../../../../../../hololive-shared/pkg/service/notification",
	"../../../../../../hololive-shared/pkg/service/cache",
	"../../../../../../hololive-shared/pkg/service/delivery",
	"../../../../../../hololive-shared/pkg/service/lease",
}

var bannedLogAttrKeys = map[string]string{
	"user_name":   "Kakao 닉네임",
	"room_name":   "Kakao 방 제목",
	"sender":      "Kakao 닉네임",
	"query":       "사용자 입력 검색어",
	"sub_command": "사용자 입력 원문",
}

// 이 key들은 값 자체가 비-canonical일 수 있어 privacylog 헬퍼만 만들 수 있다. key 이름 재도입이
// 아니라 "허용 key에 실린 raw 값"이 이번 회귀의 본체라, literal key 사용 자체를 금지한다.
var privacylogOnlyAttrKeys = map[string]string{
	KeyRoomID:     "privacylog.RoomIDAttr/RoomAttr",
	KeyChatID:     "privacylog.ChatIDAttr/ChatAttr",
	KeyCacheKey:   "privacylog.CacheKeyAttr",
	KeyCacheField: "privacylog.CacheFieldAttr",
}

var slogAttrConstructors = map[string]struct{}{
	"String": {}, "Int": {}, "Int64": {}, "Uint64": {}, "Float64": {}, "Bool": {},
	"Time": {}, "Duration": {}, "Any": {}, "Group": {},
}

var slogLevelMethods = map[string]struct{}{
	"Debug": {}, "Info": {}, "Warn": {}, "Error": {}, "Log": {},
	"DebugContext": {}, "InfoContext": {}, "WarnContext": {}, "ErrorContext": {},
	"LogAttrs": {},
}

// 이 facade들은 가변인자가 slog.Attr라 loose-kv 스캔 대상이 아니다. 스캔하면 event/message 리터럴이
// key로 잘못 수집된다.
var structuredLogFacades = map[string]struct{}{
	"sharedlog": {}, "sharedlogging": {}, "logging": {},
}

type logAttrKeyUse struct {
	key      string
	expr     string
	position string
}

func TestBotPlaneLogCallsitesRejectBannedAttrKeys(t *testing.T) {
	t.Parallel()

	uses := collectBotPlaneLogAttrKeys(t)
	if len(uses) < 150 {
		t.Fatalf("collected only %d log attr keys; the scan is not reaching every scanned root", len(uses))
	}

	seen := make(map[string]struct{}, len(uses))
	for _, use := range uses {
		seen[use.key] = struct{}{}
	}
	for _, report := range attrKeyViolations(uses) {
		t.Error(report)
	}

	for _, canary := range []string{"user_id", "command", "message_len", "channel_id", "pool_size", "lease"} {
		if _, ok := seen[canary]; !ok {
			t.Fatalf("canonical key %q was not collected; the scan is not reading log callsites", canary)
		}
	}
}

func attrKeyViolations(uses []logAttrKeyUse) []string {
	var reports []string
	reported := make(map[logAttrKeyUse]struct{}, len(uses))
	for _, use := range uses {
		if _, duplicate := reported[use]; duplicate {
			continue
		}
		reported[use] = struct{}{}

		if use.key == "" {
			reports = append(reports, fmt.Sprintf(
				"%s: log attr key %s is not statically resolvable, so this gate cannot tell which key it introduces; "+
					"use a string literal, a const in the same directory, or a privacylog helper",
				use.position, use.expr))

			continue
		}
		if reason, banned := bannedLogAttrKeys[use.key]; banned {
			reports = append(reports, fmt.Sprintf("%s: log attr key %q is banned (%s)", use.position, use.key, reason))
		}
		if helper, restricted := privacylogOnlyAttrKeys[use.key]; restricted {
			reports = append(reports, fmt.Sprintf(
				"%s: log attr key %q must be built by %s, not by a literal key", use.position, use.key, helper))
		}
		if strings.HasSuffix(use.key, "sha256_8") {
			reports = append(reports, fmt.Sprintf(
				"%s: log attr key %q is banned (저엔트로피 입력의 unsalted digest)", use.position, use.key))
		}
	}

	return reports
}

func TestPrivacylogOwnedAttrKeysStayRestricted(t *testing.T) {
	t.Parallel()

	for _, key := range []string{KeyRoomID, KeyChatID, KeyCacheKey, KeyCacheField} {
		if _, restricted := privacylogOnlyAttrKeys[key]; !restricted {
			t.Errorf("attr key %q is built by a privacylog helper but is absent from privacylogOnlyAttrKeys; "+
				"a raw literal on this key would carry Kakao plaintext past this gate unreported", key)
		}
	}
}

func TestScannerCatchesEveryBypassShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "raw value on an allowed key",
			source: `package p; func f(){ logger.Info("m", slog.String("room_id", cmdCtx.Room)) }`,
			want:   "room_id",
		},
		{
			name:   "context variant shifts the key parity",
			source: `package p; func f(){ logger.InfoContext(ctx, "m", "user_name", v, "sender", v) }`,
			want:   "user_name",
		},
		{
			name:   "map composite literal inside slog.Any",
			source: `package p; func f(){ logger.Info("m", slog.Any("payload", map[string]string{"user_name": v})) }`,
			want:   "user_name",
		},
		{
			name:   "const alias as the key",
			source: `package p; const bannedAlias = "room_name"; func f(){ logger.Info("m", slog.String(bannedAlias, v)) }`,
			want:   "room_name",
		},
		{
			name:   "LogAttrs shifts the key parity",
			source: `package p; func f(){ logger.LogAttrs(ctx, slog.LevelWarn, "m", slog.String("chat_id", raw)) }`,
			want:   "chat_id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !scanSourceCollectsKey(t, tc.source, tc.want) {
				t.Fatalf("scanner missed %q in %s", tc.want, tc.source)
			}
		})
	}
}

func TestScannerFailsClosedOnUnresolvableKeyExpressions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
	}{
		{
			name:   "selector const key",
			source: `package p; func f(){ logger.Info("m", slog.String(privacylog.KeyRoomID, cmdCtx.Room)) }`,
		},
		{
			name:   "cross-package const key",
			source: `package p; func f(){ logger.Info("m", slog.String(shared.KeyUserName, v)) }`,
		},
		{
			name:   "var key",
			source: `package p; var dynamicKey = "room_id"; func f(){ logger.Info("m", slog.String(dynamicKey, v)) }`,
		},
		{
			name:   "loose selector const key",
			source: `package p; func f(){ logger.Warn("m", privacylog.KeyChatID, raw) }`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if reports := scanSourceViolations(t, tc.source); len(reports) == 0 {
				t.Fatalf("scanner accepted an unresolvable key expression in %s", tc.source)
			}
		})
	}
}

func TestLooseKeyValueStartMatchesSlogSignatures(t *testing.T) {
	t.Parallel()

	loggerType := reflect.TypeOf(&slog.Logger{})
	for method := range slogLevelMethods {
		signature, ok := loggerType.MethodByName(method)
		if !ok {
			t.Fatalf("slog.Logger has no method %q; the scanner models a signature that does not exist", method)
		}

		want := signature.Type.NumIn() - 2
		if got := looseKeyValueStart(method); got != want {
			t.Errorf("looseKeyValueStart(%q) = %d, want %d (%s)", method, got, want, signature.Type)
		}
	}
}

func TestScannerCatchesBannedKeyBehindLogParity(t *testing.T) {
	t.Parallel()

	source := `package p; func f(){ logger.Log(ctx, slog.LevelWarn, "m", "user_name", v) }`
	if !scanSourceCollectsKey(t, source, "user_name") {
		t.Fatalf("scanner missed the banned key in %s", source)
	}
}

func TestScannerIgnoresFacadeEventAndMessageLiterals(t *testing.T) {
	t.Parallel()

	source := `package p; func f(){ sharedlog.Warn(ctx, logger, EventX, "room_name", slog.String("user_id", v)) }`
	if scanSourceCollectsKey(t, source, "room_name") {
		t.Fatal("facade message literal must not be collected as an attr key")
	}
	if !scanSourceCollectsKey(t, source, "user_id") {
		t.Fatal("facade attr arguments must still be collected")
	}
}

func scanSourceCollectsKey(t *testing.T, source, key string) bool {
	t.Helper()

	for _, use := range scanSource(t, source) {
		if use.key == key {
			return true
		}
	}

	return false
}

func scanSourceViolations(t *testing.T, source string) []string {
	t.Helper()

	return attrKeyViolations(scanSource(t, source))
}

func scanSource(t *testing.T, source string) []logAttrKeyUse {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "inject.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse injected source: %v", err)
	}

	constants := map[string]string{}
	collectStringConstants("inject", file, constants)

	var uses []logAttrKeyUse
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		uses = append(uses, logAttrKeysFromCall("inject", fileSet, call, constants)...)

		return true
	})

	return uses
}

func collectBotPlaneLogAttrKeys(t *testing.T) []logAttrKeyUse {
	t.Helper()

	fileSet := token.NewFileSet()
	sources := parseScannedRoots(t, fileSet)

	constants := map[string]string{}
	for path, file := range sources {
		collectStringConstants(filepath.Dir(path), file, constants)
	}

	var uses []logAttrKeyUse
	for path, file := range sources {
		scope := filepath.Dir(path)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			uses = append(uses, logAttrKeysFromCall(scope, fileSet, call, constants)...)

			return true
		})
	}

	return uses
}

func parseScannedRoots(t *testing.T, fileSet *token.FileSet) map[string]*ast.File {
	t.Helper()

	sources := map[string]*ast.File{}
	for _, root := range scannedRoots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			file, parseErr := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			sources[path] = file

			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	return sources
}

func collectStringConstants(scope string, file *ast.File, out map[string]string) {
	for _, decl := range file.Decls {
		generic, ok := decl.(*ast.GenDecl)
		if !ok || generic.Tok != token.CONST {
			continue
		}

		for _, spec := range generic.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range values.Names {
				if index >= len(values.Values) {
					continue
				}
				if literal, ok := stringLiteralValue(values.Values[index]); ok {
					out[scope+"."+name.Name] = literal
				}
			}
		}
	}
}

func logAttrKeysFromCall(scope string, fileSet *token.FileSet, call *ast.CallExpr, constants map[string]string) []logAttrKeyUse {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	receiver, hasReceiver := selector.X.(*ast.Ident)
	if hasReceiver && receiver.Name == "slog" {
		if _, isAttr := slogAttrConstructors[selector.Sel.Name]; isAttr {
			var uses []logAttrKeyUse
			if len(call.Args) > 0 {
				uses = append(uses, keyUse(scope, fileSet, call.Args[0], constants))
			}

			return append(uses, compositeLiteralKeys(scope, fileSet, call.Args, constants)...)
		}
	}
	if hasReceiver {
		if _, facade := structuredLogFacades[receiver.Name]; facade {
			return nil
		}
	}

	if _, isLevel := slogLevelMethods[selector.Sel.Name]; !isLevel {
		return nil
	}

	var uses []logAttrKeyUse
	for _, index := range looseKeyIndexes(call, selector.Sel.Name) {
		uses = append(uses, keyUse(scope, fileSet, call.Args[index], constants))
	}

	return append(uses, compositeLiteralKeys(scope, fileSet, call.Args, constants)...)
}

// slog는 loose key/value 쌍과 slog.Attr을 한 호출에 섞을 수 있어 고정 stride로는 parity가 어긋난다.
func looseKeyIndexes(call *ast.CallExpr, method string) []int {
	var indexes []int
	for index := looseKeyValueStart(method); index < len(call.Args); {
		if _, isAttrCall := call.Args[index].(*ast.CallExpr); isAttrCall {
			index++

			continue
		}
		if call.Ellipsis.IsValid() && index == len(call.Args)-1 {
			index++

			continue
		}

		indexes = append(indexes, index)
		index += 2
	}

	return indexes
}

func looseKeyValueStart(method string) int {
	if method == "Log" || method == "LogAttrs" {
		return 3
	}
	if strings.HasSuffix(method, "Context") {
		return 2
	}

	return 1
}

func compositeLiteralKeys(scope string, fileSet *token.FileSet, args []ast.Expr, constants map[string]string) []logAttrKeyUse {
	var uses []logAttrKeyUse
	for _, arg := range args {
		ast.Inspect(arg, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := resolveKey(scope, pair.Key, constants); ok {
					uses = append(uses, logAttrKeyUse{key: key, position: fileSet.Position(pair.Pos()).String()})
				}
			}

			return true
		})
	}

	return uses
}

func keyUse(scope string, fileSet *token.FileSet, expr ast.Expr, constants map[string]string) logAttrKeyUse {
	position := fileSet.Position(expr.Pos()).String()
	if key, ok := resolveKey(scope, expr, constants); ok {
		return logAttrKeyUse{key: key, position: position}
	}

	return logAttrKeyUse{expr: formatExpr(fileSet, expr), position: position}
}

func formatExpr(fileSet *token.FileSet, expr ast.Expr) string {
	var rendered strings.Builder
	if err := printer.Fprint(&rendered, fileSet, expr); err != nil {
		return "<unprintable>"
	}

	return rendered.String()
}

func resolveKey(scope string, expr ast.Expr, constants map[string]string) (string, bool) {
	if literal, ok := stringLiteralValue(expr); ok {
		return literal, true
	}
	if identifier, ok := expr.(*ast.Ident); ok {
		if value, ok := constants[scope+"."+identifier.Name]; ok {
			return value, true
		}
	}

	return "", false
}

func stringLiteralValue(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}

	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}

	return value, true
}
