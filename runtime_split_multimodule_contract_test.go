package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeSplitStandaloneModulesContract(t *testing.T) {
	t.Parallel()

	mustExist := []string{
		"hololive/hololive-admin-api/go.mod",
		"hololive/hololive-admin-api/cmd/admin-api/main.go",
		"hololive/hololive-alarm-worker/go.mod",
		"hololive/hololive-alarm-worker/cmd/alarm-worker/main.go",
		"hololive/hololive-shared/pkg/service/notification/alarm_service.go",
	}
	for _, path := range mustExist {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	mustNotExist := []string{
		"hololive/hololive-kakao-bot-go/cmd/admin-api",
		"hololive/hololive-kakao-bot-go/cmd/alarm-worker",
		"hololive/hololive-kakao-bot-go/internal/server",
		"hololive/hololive-kakao-bot-go/internal/service/alarm/checker",
		"hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler",
		"hololive/hololive-kakao-bot-go/internal/service/system",
		"hololive/hololive-kakao-bot-go/internal/service/trigger",
		"hololive/hololive-kakao-bot-go/internal/service/notification",
	}
	for _, path := range mustNotExist {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("expected %s to be removed from hololive-kakao-bot-go ownership", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
	}

	goWork := readContractFile(t, "go.work")
	for _, entry := range []string{"./hololive/hololive-admin-api", "./hololive/hololive-alarm-worker"} {
		if !strings.Contains(goWork, entry) {
			t.Fatalf("go.work must include %s", entry)
		}
	}

	projectMap := readContractFile(t, filepath.ToSlash("docs/current/PROJECT_MAP.md"))
	for _, want := range []string{
		"| `hololive-admin-api` | Go 1.26.2 | `hololive/hololive-admin-api/` |",
		"| `hololive-alarm-worker` | Go 1.26.2 | `hololive/hololive-alarm-worker/` |",
	} {
		if !strings.Contains(projectMap, want) {
			t.Fatalf("project map must contain %q", want)
		}
	}
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
