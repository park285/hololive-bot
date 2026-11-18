package youtubejs

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"
)

type helperLeakSnap struct {
	fds        int
	children   int
	dirs       int
	goroutines int
}

func TestHLPLeak(t *testing.T) {
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		t.Skip("skip helper leak slope: /proc is unavailable")
	}

	const isolated = "YOUTUBEJS_HELPER_LEAK_PROCESS"

	if os.Getenv(isolated) != "1" {
		cmd := exec.CommandContext(t.Context(), "/proc/self/exe", "-test.run=^TestHLPLeak$")

		cmd.Env = append(os.Environ(), isolated+"=1", "GOMAXPROCS="+strconv.Itoa(runtime.GOMAXPROCS(0)))
		cmd.WaitDelay = 5 * time.Second

		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated helper leak test: %v\n%s", err, output)
		}

		return
	}

	base := shortRuntimeBase(t)

	const (
		warmup = 10
		cycles = 100
	)

	runHelperLeakCycles(t, base, warmup)

	before := measureHelperLeak(t, base)

	runHelperLeakCycles(t, base, cycles)

	after := measureHelperLeak(t, base)
	if after != before {
		for _, name := range []string{"goroutineleak", "goroutine"} {
			var profile bytes.Buffer

			if err := pprof.Lookup(name).WriteTo(&profile, 1); err != nil {
				t.Errorf("write %s profile: %v", name, err)

				continue
			}

			t.Logf("%s", profile.String())
		}

		t.Fatalf("helper leak slope after %d cycles: before=%+v after=%+v", cycles, before, after)
	}
}

func runHelperLeakCycles(t *testing.T, base string, count int) {
	t.Helper()

	pprof.Do(t.Context(), pprof.Labels("helper_leak_cycle", "active"), func(ctx context.Context) {
		for range count {
			runFixtureCycle(ctx, t, base)
		}
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var profile bytes.Buffer

	for {
		profile.Reset()

		if err := pprof.Lookup("goroutine").WriteTo(&profile, 1); err != nil {
			t.Fatal(err)
		}

		if !bytes.Contains(profile.Bytes(), []byte(`"helper_leak_cycle":"active"`)) {
			return
		}

		if ctx.Err() != nil {
			t.Fatalf("helper goroutines still running after close:\n%s", &profile)
		}

		runtime.Gosched()
	}
}

func runFixtureCycle(parent context.Context, t *testing.T, base string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(parent, 10*time.Second)

	defer cancel()

	helper, _, err := Start(ctx, fixtureConfig(t, base, "ready"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	defer func() {
		if err := helper.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}

		select {
		case <-helper.Done():
		default:
			t.Error("helper done not closed after Close")
		}
	}()

	if err := helper.Healthy(ctx); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
}

func measureHelperLeak(t *testing.T, base string) helperLeakSnap {
	t.Helper()
	runtime.GC()
	runtime.GC()

	return helperLeakSnap{
		fds:        countProcDir(t, "/proc/self/fd"),
		children:   countChildPIDs(t),
		dirs:       countRuntimeDirs(t, base),
		goroutines: runtime.NumGoroutine(),
	}
}

func countProcDir(t *testing.T, path string) int {
	t.Helper()

	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return len(entries)
}

func countRuntimeDirs(t *testing.T, base string) int {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(base, helperRuntimeDirPrefix+"*"))
	if err != nil {
		t.Fatal(err)
	}

	return len(matches)
}

func countChildPIDs(t *testing.T) int {
	t.Helper()

	self := os.Getpid()
	procRoot := "/proc"

	entries, err := os.ReadDir(procRoot)
	if err != nil {
		t.Fatalf("read /proc: %v", err)
	}

	count := 0

	for _, entry := range entries {
		if !isProcPID(entry.Name()) {
			continue
		}

		raw, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "stat"))
		if err != nil {
			continue
		}

		ppid, ok := procPPID(raw)
		if ok && ppid == self {
			count++
		}
	}

	return count
}

func isProcPID(name string) bool {
	if name == "" {
		return false
	}

	for i := range len(name) {
		if name[i] < '0' || name[i] > '9' {
			return false
		}
	}

	return true
}

func procPPID(stat []byte) (int, bool) {
	_, after, found := bytes.CutLast(stat, []byte(")"))
	if !found || len(after) < 2 {
		return 0, false
	}

	fields := strings.Fields(string(after[1:]))
	if len(fields) < 2 {
		return 0, false
	}

	ppid, err := strconv.Atoi(fields[1])

	return ppid, err == nil
}
