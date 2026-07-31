package keys

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

// privacylog.RedactCacheKey는 등록되지 않은 접두를 원문 그대로 남긴다. 그 fail-open을 감당할 수 있는
// 근거가 이 테스트다: room 인자를 받는 Build*가 새로 생기면 여기서 먼저 깨진다.
const (
	redactionRoomID    = "공지: 상대방닉네임 님과의 대화"
	redactionStreamID  = "dQw4w9WgXcQ"
	redactionChannelID = "UC1DCedRgGHBdm81E1llLhOQ"
	redactionCategory  = "10m"
)

func roomBearingKeyBuilders() map[string]func() string {
	scheduled := time.Unix(1785499200, 0).UTC()
	rescheduled := scheduled.Add(time.Hour)

	return map[string]func() string{
		"BuildRoomAlarmKey": func() string {
			return BuildRoomAlarmKey(redactionRoomID)
		},
		"BuildNotifyClaimKey": func() string {
			return BuildNotifyClaimKey(redactionRoomID, redactionStreamID, scheduled, redactionCategory)
		},
		"BuildLogicalEventClaimKey": func() string {
			return BuildLogicalEventClaimKey(redactionRoomID, redactionChannelID, redactionStreamID, "제목", scheduled, redactionCategory)
		},
		"BuildUpcomingEventKey": func() string {
			return BuildUpcomingEventKey(redactionRoomID, redactionChannelID, redactionStreamID, "제목", scheduled)
		},
		"BuildRoomScheduleTransitionKey": func() string {
			return BuildRoomScheduleTransitionKey(redactionRoomID, redactionStreamID, scheduled, rescheduled)
		},
		"BuildLogicalScheduleIndexKey": func() string {
			return BuildLogicalScheduleIndexKey(redactionRoomID, redactionChannelID, redactionStreamID, "제목")
		},
		"BuildLogicalScheduleTransitionKey": func() string {
			return BuildLogicalScheduleTransitionKey(redactionRoomID, redactionChannelID, redactionStreamID, "제목", scheduled, rescheduled)
		},
	}
}

func TestRoomBearingKeysAreRedactedForLogs(t *testing.T) {
	t.Parallel()

	for name, build := range roomBearingKeyBuilders() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			key := build()
			if !strings.Contains(key, redactionRoomID) {
				t.Fatalf("%s did not interpolate the room identifier; the fixture no longer exercises the leak", name)
			}

			redacted := privacylog.RedactCacheKey(key)
			if strings.Contains(redacted, redactionRoomID) {
				t.Fatalf("%s: privacylog.RedactCacheKey(%q) = %q leaks the room identifier; "+
					"register the key prefix in privacylog identifierKeyRules", name, key, redacted)
			}
			if redacted == key {
				t.Fatalf("%s: privacylog.RedactCacheKey left %q untouched", name, key)
			}
		})
	}
}

func TestEveryRoomBearingBuilderIsCovered(t *testing.T) {
	t.Parallel()

	covered := roomBearingKeyBuilders()
	for _, name := range exportedRoomKeyBuilders(t) {
		if _, ok := covered[name]; !ok {
			t.Errorf("%s takes a roomID but has no redaction fixture; add it to roomBearingKeyBuilders "+
				"so privacylog coverage is proven for the key shape it builds", name)
		}
	}
}

func exportedRoomKeyBuilders(t *testing.T) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	var names []string

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
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
		names = append(names, roomKeyBuilderNames(file)...)

		return nil
	})
	if err != nil {
		t.Fatalf("walk keys package: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no roomID-taking builder was found; the scan is not reading the keys package")
	}

	return names
}

func roomKeyBuilderNames(file *ast.File) []string {
	var names []string
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv != nil || !function.Name.IsExported() || !returnsString(function) {
			continue
		}
		if takesRoomIdentifier(function) {
			names = append(names, function.Name.Name)
		}
	}

	return names
}

func takesRoomIdentifier(function *ast.FuncDecl) bool {
	for _, field := range function.Type.Params.List {
		for _, name := range field.Names {
			if strings.EqualFold(name.Name, "roomID") {
				return true
			}
		}
	}

	return false
}

func returnsString(function *ast.FuncDecl) bool {
	if function.Type.Results == nil || len(function.Type.Results.List) != 1 {
		return false
	}
	identifier, ok := function.Type.Results.List[0].Type.(*ast.Ident)

	return ok && identifier.Name == "string"
}
