package youtubejs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
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

	base := t.TempDir()

	const (
		warmup = 10
		cycles = 100
	)

	for range warmup {
		runFixtureCycle(t, base)
	}

	before := measureHelperLeak(t, base)

	for range cycles {
		runFixtureCycle(t, base)
	}

	after := measureHelperLeak(t, base)
	if after != before {
		t.Fatalf("helper leak slope after %d cycles: before=%+v after=%+v", cycles, before, after)
	}
}

func runFixtureCycle(t *testing.T, base string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	defer cancel()

	helper, _, err := Start(ctx, fixtureConfig(t, base, "ready"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := helper.Healthy(ctx); err != nil {
		t.Fatalf("Healthy: %v", err)
	}

	if err := helper.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-helper.Done():
	case <-time.After(time.Second):
		t.Fatal("helper done not closed")
	}
}

func measureHelperLeak(t *testing.T, base string) helperLeakSnap {
	t.Helper()
	runtime.GC()
	runtime.GC()
	time.Sleep(20 * time.Millisecond)

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
