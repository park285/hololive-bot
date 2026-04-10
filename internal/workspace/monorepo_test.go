package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMonorepoModuleSuites(t *testing.T) {
	if os.Getenv("HOLOLIVE_WORKSPACE_MONOREPO_TEST") == "1" {
		t.Skip("already running monorepo workspace suite")
	}

	cmd := exec.Command(
		"go",
		"test",
		"./shared-go/...",
		"./hololive/hololive-shared/...",
		"./hololive/hololive-dispatcher-go/...",
		"./hololive/hololive-kakao-bot-go/...",
		"./hololive/hololive-llm-sched/...",
		"./hololive/hololive-stream-ingester/...",
	)
	cmd.Env = append(os.Environ(), "HOLOLIVE_WORKSPACE_MONOREPO_TEST=1")
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve workspace test file path")
	}
	cmd.Dir = filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("monorepo go test suites failed: %v", err)
	}
}
