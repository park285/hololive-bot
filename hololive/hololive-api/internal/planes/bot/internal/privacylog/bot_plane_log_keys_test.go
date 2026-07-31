package privacylog

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const botPlaneRoot = "../.."

var bannedLogAttrKeys = map[string]string{
	"user_name":   "Kakao 닉네임",
	"room_name":   "Kakao 방 제목",
	"sender":      "Kakao 닉네임",
	"query":       "사용자 입력 검색어",
	"sub_command": "사용자 입력 원문",
}

var slogAttrConstructors = map[string]struct{}{
	"String": {}, "Int": {}, "Int64": {}, "Uint64": {}, "Float64": {}, "Bool": {},
	"Time": {}, "Duration": {}, "Any": {}, "Group": {},
}

var slogLevelMethods = map[string]struct{}{
	"Debug": {}, "Info": {}, "Warn": {}, "Error": {}, "Log": {},
	"DebugContext": {}, "InfoContext": {}, "WarnContext": {}, "ErrorContext": {},
}

type logAttrKeyUse struct {
	key      string
	position string
}

func TestBotPlaneLogCallsitesRejectBannedAttrKeys(t *testing.T) {
	t.Parallel()

	uses := collectBotPlaneLogAttrKeys(t)
	if len(uses) < 50 {
		t.Fatalf("collected only %d log attr keys; the scan is not reaching the bot plane", len(uses))
	}

	seen := make(map[string]struct{}, len(uses))
	for _, use := range uses {
		seen[use.key] = struct{}{}

		if reason, banned := bannedLogAttrKeys[use.key]; banned {
			t.Errorf("%s: log attr key %q is banned (%s)", use.position, use.key, reason)
		}
		if strings.HasSuffix(use.key, "sha256_8") {
			t.Errorf("%s: log attr key %q is banned (저엔트로피 입력의 unsalted digest)", use.position, use.key)
		}
	}

	for _, canary := range []string{"user_id", "command", "message_len"} {
		if _, ok := seen[canary]; !ok {
			t.Fatalf("canonical key %q was not collected; the scan is not reading log callsites", canary)
		}
	}
}

func collectBotPlaneLogAttrKeys(t *testing.T) []logAttrKeyUse {
	t.Helper()

	var uses []logAttrKeyUse
	fileSet := token.NewFileSet()

	err := filepath.WalkDir(botPlaneRoot, func(path string, entry fs.DirEntry, err error) error {
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

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			uses = append(uses, logAttrKeysFromCall(fileSet, call)...)

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk bot plane: %v", err)
	}

	return uses
}

func logAttrKeysFromCall(fileSet *token.FileSet, call *ast.CallExpr) []logAttrKeyUse {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == "slog" {
		if _, isAttr := slogAttrConstructors[selector.Sel.Name]; isAttr {
			if key, ok := stringLiteral(call.Args, 0); ok {
				return []logAttrKeyUse{{key: key, position: fileSet.Position(call.Pos()).String()}}
			}

			return nil
		}
	}

	if _, isLevel := slogLevelMethods[selector.Sel.Name]; !isLevel {
		return nil
	}

	var uses []logAttrKeyUse
	for index := 1; index < len(call.Args); index += 2 {
		if key, ok := stringLiteral(call.Args, index); ok {
			uses = append(uses, logAttrKeyUse{key: key, position: fileSet.Position(call.Args[index].Pos()).String()})
		}
	}

	return uses
}

func stringLiteral(args []ast.Expr, index int) (string, bool) {
	if index >= len(args) {
		return "", false
	}

	literal, ok := args[index].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}

	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}

	return value, true
}
