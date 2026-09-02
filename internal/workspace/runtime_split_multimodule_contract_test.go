package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeSplitStandaloneModulesContract(t *testing.T) {
	t.Parallel()

	root := repoRootFromHelper(t)

	mustExist := []string{
		"hololive/hololive-api/go.mod",
		"hololive/hololive-api/cmd/hololive-api/main.go",
		"hololive/hololive-api/internal/planes/admin/app/runtime_admin_api.go",
		"hololive/hololive-alarm-worker/go.mod",
		"hololive/hololive-alarm-worker/cmd/alarm-worker/main.go",
		"hololive/hololive-shared/pkg/service/notification/alarmservice/alarm_service.go",
	}
	for _, path := range mustExist {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}

	goWork := readRepoFile(t, root, "go.work")

	for _, entry := range []string{"./hololive/hololive-api", "./hololive/hololive-alarm-worker"} {
		if !strings.Contains(goWork, entry) {
			t.Fatalf("go.work must include %s", entry)
		}
	}
}
