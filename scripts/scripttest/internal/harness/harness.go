package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

type Cmd struct {
	Script string
	Args   []string
	Env    []string
	Dir    string
	Stdin  string
	Bin    string
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type sessionSet struct {
	mu  sync.Mutex
	ids map[int]struct{}
}

var testSessions sync.Map

var bashPath = sync.OnceValues(func() (string, error) {
	return exec.LookPath("bash")
})

func (r Result) String() string {
	return fmt.Sprintf("exit %d\nstdout:\n%s\nstderr:\n%s", r.ExitCode, r.Stdout, r.Stderr)
}

var repoRoot = sync.OnceValues(func() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("locate repository root: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "scripts", "scripttest", "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("repository root not found above the test directory")
		}

		dir = parent
	}
})

func Root(tb testing.TB) string {
	tb.Helper()

	root, err := repoRoot()
	if err != nil {
		tb.Fatalf("locate repository root: %v", err)
	}

	return root
}

func Path(tb testing.TB, rel string) string {
	tb.Helper()

	return filepath.Join(Root(tb), filepath.FromSlash(rel))
}

func Bash(tb testing.TB, c Cmd) Result {
	tb.Helper()

	return run(tb.Context(), tb, c)
}

func CleanupBash(tb testing.TB, c Cmd) {
	tb.Helper()

	ctx := context.WithoutCancel(tb.Context())
	tb.Cleanup(func() {
		if r := run(ctx, tb, c); r.ExitCode != 0 {
			tb.Errorf("cleanup: %s", r)
		}
	})
}

func run(ctx context.Context, tb testing.TB, c Cmd) Result {
	tb.Helper()

	args := append([]string{"-c", "set -euo pipefail\n" + c.Script, "bash"}, c.Args...)
	cmd := exec.CommandContext(ctx, "bash", args...) // #nosec G204 -- 테스트가 저장소 스크립트를 실행하는 단일 진입점이다.

	cmd.Dir = c.Dir

	cmd.Env = append(os.Environ(), "SCRIPTTEST_ROOT="+Root(tb))

	if c.Bin != "" {
		cmd.Env = append(cmd.Env, "PATH="+c.Bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	cmd.Env = append(cmd.Env, c.Env...)
	cmd.Stdin = strings.NewReader(c.Stdin)

	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	result := Result{}

	if err := cmd.Start(); err != nil {
		tb.Fatalf("start bash: %v", err)
	}

	sid := cmd.Process.Pid

	TrackSession(tb, sid)

	if err := cmd.Wait(); err != nil {
		exitErr, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			tb.Fatalf("run bash: %v", err)
		}

		result.ExitCode = exitErr.ExitCode()
	}

	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	return result
}

func TrackSession(tb testing.TB, sid int) {
	tb.Helper()

	value, _ := testSessions.LoadOrStore(tb, &sessionSet{ids: make(map[int]struct{})})

	sessions, ok := value.(*sessionSet)
	if !ok {
		tb.Fatalf("session registry holds %T", value)
	}

	sessions.mu.Lock()

	sessions.ids[sid] = struct{}{}
	sessions.mu.Unlock()

	tb.Cleanup(func() {
		value, loaded := testSessions.LoadAndDelete(tb)
		if !loaded {
			return
		}

		sessions, ok := value.(*sessionSet)
		if !ok {
			tb.Errorf("session registry holds %T", value)

			return
		}

		sessions.mu.Lock()
		defer sessions.mu.Unlock()

		killSessions(tb, sessions.ids)
	})
}

func killSessions(tb testing.TB, sessions map[int]struct{}) {
	tb.Helper()

	entries, err := os.ReadDir("/proc")
	if err != nil {
		tb.Errorf("list processes: %v", err)

		return
	}

	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat")) // #nosec G304 -- procfs 경로만 읽는다.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue
		}

		if err != nil {
			tb.Errorf("read process %d: %v", pid, err)

			continue
		}

		fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
		if len(fields) < 4 {
			continue
		}

		sid, err := strconv.Atoi(fields[3])
		if err != nil {
			continue
		}

		if _, tracked := sessions[sid]; !tracked {
			continue
		}

		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			tb.Errorf("kill process %d of session %d: %v", pid, sid, err)
		}
	}
}

func Succeeds(tb testing.TB, label string, r Result) {
	tb.Helper()

	if r.ExitCode != 0 {
		tb.Fatalf("%s: want success, got %s", label, r)
	}
}

func Fails(tb testing.TB, label string, r Result) {
	tb.Helper()

	if r.ExitCode == 0 || r.ExitCode == 126 || r.ExitCode == 127 {
		tb.Fatalf("%s: want a rejection, got %s", label, r)
	}
}

func Write(tb testing.TB, path, content string) {
	tb.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		tb.Fatalf("create parent of %s: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		tb.Fatalf("write %s: %v", path, err)
	}
}

func Executable(tb testing.TB, path, body string) {
	tb.Helper()

	bash, err := bashPath()
	if err != nil {
		tb.Fatalf("locate bash: %v", err)
	}

	Write(tb, path, "#!"+bash+"\n"+body)

	if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- 가짜 명령은 PATH에서 실행할 수 있어야 한다.
		tb.Fatalf("chmod %s: %v", path, err)
	}
}

func Read(tb testing.TB, path string) string {
	tb.Helper()

	data, err := os.ReadFile(path) // #nosec G304 -- 테스트가 만든 fixture 경로만 읽는다.
	if err != nil {
		tb.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

func Mode(tb testing.TB, path string) os.FileMode {
	tb.Helper()

	info, err := os.Lstat(path)
	if err != nil {
		tb.Fatalf("stat %s: %v", path, err)
	}

	return info.Mode()
}

func Symlink(tb testing.TB, target, path string) {
	tb.Helper()

	if err := os.Symlink(target, path); err != nil {
		tb.Fatalf("symlink %s -> %s: %v", path, target, err)
	}
}

func Remove(tb testing.TB, path string) {
	tb.Helper()

	if err := os.Remove(path); err != nil {
		tb.Fatalf("remove %s: %v", path, err)
	}
}

func Size(tb testing.TB, path string) int64 {
	tb.Helper()

	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}

	if err != nil {
		tb.Fatalf("stat %s: %v", path, err)
	}

	return info.Size()
}
