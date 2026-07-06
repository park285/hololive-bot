package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func removedRuntimeName() string {
	return "settlement" + "-go"
}

func removedCommandToken(name string) string {
	return "Command" + name
}

func removedCommandValue(name string) string {
	return "settlement" + "_" + name
}

func removedRoomEnv() string {
	return "SETTLEMENT" + "_ROOM_ID"
}

func repoRootFromHelper(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve workspace helper path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func repoFS(root string) fs.FS {
	return os.DirFS(root)
}

func cleanRepoRel(t *testing.T, rel string) string {
	t.Helper()

	cleaned := filepath.ToSlash(filepath.Clean(rel))
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(cleaned) {
		t.Fatalf("invalid repo-relative path %q", rel)
	}
	return cleaned
}

func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()

	data, err := fs.ReadFile(repoFS(root), cleanRepoRel(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}

	return string(data)
}

func assertFileMissingToken(t *testing.T, root, rel, token string) {
	t.Helper()

	if strings.Contains(readRepoFile(t, root, rel), token) {
		t.Fatalf("%s still contains %q", rel, token)
	}
}

func assertOptionalFileMissingToken(t *testing.T, root, rel, token string) {
	t.Helper()

	data, err := fs.ReadFile(repoFS(root), cleanRepoRel(t, rel))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read %s: %v", rel, err)
	}
	if strings.Contains(string(data), token) {
		t.Fatalf("%s still contains %q", rel, token)
	}
}

func TestSettlementRuntimeIsRemovedFromWorkspaceInventory(t *testing.T) {
	t.Parallel()

	root := repoRootFromHelper(t)

	checks := []struct {
		file  string
		token string
	}{
		{file: "go.work", token: "./hololive/" + removedRuntimeName()},
		{file: "README.md", token: removedRuntimeName()},
		{file: "docs/current/PROJECT_MAP.md", token: removedRuntimeName()},
		{file: "internal/workspace/monorepo_test.go", token: "./hololive/" + removedRuntimeName() + "/..."},
	}

	for _, check := range checks {
		t.Run(check.file, func(t *testing.T) {
			t.Parallel()
			assertFileMissingToken(t, root, check.file, check.token)
		})
	}

	t.Run("AGENTS.md", func(t *testing.T) {
		t.Parallel()
		assertOptionalFileMissingToken(t, root, "AGENTS.md", "settlement service")
	})
}

func TestSettlementRuntimeArtifactsAreRemovedOrArchived(t *testing.T) {
	t.Parallel()

	root := repoRootFromHelper(t)

	if _, err := os.Stat(filepath.Join(root, "hololive", removedRuntimeName())); err == nil {
		t.Fatal("removed runtime directory still exists")
	}

	copyChecks := []string{
		"hololive/hololive-api/Dockerfile",
		"hololive/hololive-youtube-producer/Dockerfile",
	}
	for _, rel := range copyChecks {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			assertFileMissingToken(
				t,
				root,
				rel,
				"COPY hololive/"+removedRuntimeName()+" ./hololive/"+removedRuntimeName(),
			)
		})
	}

	residueChecks := []struct {
		file  string
		token string
	}{
		{file: "hololive/hololive-shared/pkg/domain/command.go", token: removedCommandToken("SettlementStatus")},
		{file: "hololive/hololive-shared/pkg/config/config.go", token: removedRoomEnv()},
		{file: "hololive/hololive-shared/pkg/config/internal/settings/config_types.go", token: "Settlement" + "RoomID"},
		{file: "hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd/command_normalizer_test.go", token: removedCommandValue("status")},
		{file: "hololive/hololive-api/internal/planes/bot/internal/bot/orchestration/bot_command_init_views_test.go", token: removedCommandToken("SettlementStatus")},
	}
	for _, check := range residueChecks {
		t.Run(check.file, func(t *testing.T) {
			t.Parallel()
			assertFileMissingToken(t, root, check.file, check.token)
		})
	}
}

func TestSettlementMigrationsAreArchivedAndRunbookExists(t *testing.T) {
	t.Parallel()

	root := repoRootFromHelper(t)

	activePaths := []string{
		"hololive/hololive-api/scripts/migrations/038_create_settlement.sql",
		"hololive/hololive-api/scripts/migrations/039_create_settlement_v2.sql",
	}
	for _, rel := range activePaths {
		t.Run("active-"+filepath.Base(rel), func(t *testing.T) {
			t.Parallel()
			if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
				t.Fatalf("%s should have been archived", rel)
			}
		})
	}

	requiredPaths := []string{
		"hololive/hololive-api/scripts/migrations/archive/settlement/038_create_settlement.sql",
		"hololive/hololive-api/scripts/migrations/archive/settlement/039_create_settlement_v2.sql",
		"hololive/hololive-api/scripts/migrations/manual/settlement_drop.sql",
		"docs/runbook_execution/SETTLEMENT_DECOMMISSION_RUNBOOK.md",
		"docs/history/settlement/README.md",
	}
	for _, rel := range requiredPaths {
		t.Run("required-"+filepath.Base(rel), func(t *testing.T) {
			t.Parallel()
			if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
				t.Fatalf("%s missing: %v", rel, err)
			}
		})
	}
}

func TestRemovedRuntimeGateIsWiredIntoArchitectureChecks(t *testing.T) {
	t.Parallel()

	root := repoRootFromHelper(t)

	checkScript := "scripts/architecture/check-removed-runtime-regressions.sh"
	if _, err := os.Stat(filepath.Join(root, checkScript)); err != nil {
		t.Fatalf("%s missing: %v", checkScript, err)
	}

	ciGate := readRepoFile(t, root, "scripts/architecture/ci-boundary-gate.sh")
	if !strings.Contains(ciGate, "check-removed-runtime-regressions.sh") {
		t.Fatal("ci-boundary-gate.sh is not wiring the removed runtime check")
	}
}
