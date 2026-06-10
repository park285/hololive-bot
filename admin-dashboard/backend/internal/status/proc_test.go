package status

import (
	"errors"
	"testing"
)

func withProcFile(t *testing.T, fixtures map[string][]byte) {
	t.Helper()
	old := osReadFile
	t.Cleanup(func() { osReadFile = old })
	osReadFile = func(name string) ([]byte, error) {
		if data, ok := fixtures[name]; ok {
			return data, nil
		}
		return nil, errors.New("no fixture for " + name)
	}
}

func TestMemoryStatsZeroOnReadError(t *testing.T) {
	withProcFile(t, nil)
	total, used := memoryStats()
	if total != 0 || used != 0 {
		t.Fatalf("memoryStats on read error = (%d, %d), want (0, 0)", total, used)
	}
}

func TestMemoryStatsUsedClampedWhenAvailableExceedsTotal(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/meminfo": []byte("MemTotal: 100 kB\nMemAvailable: 250 kB\n"),
	})
	total, used := memoryStats()
	if total != 100*1024 {
		t.Fatalf("total = %d, want %d", total, 100*1024)
	}
	if used != 0 {
		t.Fatalf("used = %d, want 0 when available > total", used)
	}
}

func TestMemoryStatsIgnoresShortLines(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/meminfo": []byte("MemTotal\nMemTotal: 2048 kB\nMemAvailable: 1024 kB\ngarbage\n"),
	})
	total, used := memoryStats()
	if total != 2048*1024 {
		t.Fatalf("total = %d, want %d", total, 2048*1024)
	}
	if used != 1024*1024 {
		t.Fatalf("used = %d, want %d", used, 1024*1024)
	}
}

func TestLoadAverageParsesThreeFields(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/loadavg": []byte("0.50 1.25 2.00 1/234 5678\n"),
	})
	one, five, fifteen := loadAverage()
	if one != 0.50 || five != 1.25 || fifteen != 2.00 {
		t.Fatalf("loadAverage = (%v, %v, %v), want (0.5, 1.25, 2.0)", one, five, fifteen)
	}
}

func TestLoadAverageZeroOnReadError(t *testing.T) {
	withProcFile(t, nil)
	one, five, fifteen := loadAverage()
	if one != 0 || five != 0 || fifteen != 0 {
		t.Fatalf("loadAverage on read error = (%v, %v, %v), want zeros", one, five, fifteen)
	}
}

func TestLoadAverageZeroOnTooFewFields(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/loadavg": []byte("0.5 1.0\n"),
	})
	one, five, fifteen := loadAverage()
	if one != 0 || five != 0 || fifteen != 0 {
		t.Fatalf("loadAverage with 2 fields = (%v, %v, %v), want zeros", one, five, fifteen)
	}
}

func TestThreadCountParsesStatus(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/self/status": []byte("Name:\tadmin\nThreads:\t42\nVmRSS:\t100 kB\n"),
	})
	if got := threadCount(); got != 42 {
		t.Fatalf("threadCount = %d, want 42", got)
	}
}

func TestThreadCountZeroOnReadError(t *testing.T) {
	withProcFile(t, nil)
	if got := threadCount(); got != 0 {
		t.Fatalf("threadCount on read error = %d, want 0", got)
	}
}

func TestThreadCountZeroWhenAbsent(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/self/status": []byte("Name:\tadmin\nVmRSS:\t100 kB\n"),
	})
	if got := threadCount(); got != 0 {
		t.Fatalf("threadCount without Threads line = %d, want 0", got)
	}
}

func TestReadCPUSampleParsesAggregateLine(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/stat": []byte("cpu  100 0 50 800 20 0 0 0 0 0\ncpu0 ...\n"),
	})
	idle, total, ok := readCPUSample()
	if !ok {
		t.Fatal("readCPUSample ok = false, want true")
	}
	if idle != 820 {
		t.Fatalf("idle = %d, want 820 (idle 800 + iowait 20)", idle)
	}
	if total != 970 {
		t.Fatalf("total = %d, want 970 (sum of all fields)", total)
	}
}

func TestReadCPUSampleFailsOnReadError(t *testing.T) {
	withProcFile(t, nil)
	if _, _, ok := readCPUSample(); ok {
		t.Fatal("readCPUSample ok = true on read error, want false")
	}
}

func TestReadCPUSampleFailsOnTooFewFields(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/stat": []byte("cpu 1 2 3\n"),
	})
	if _, _, ok := readCPUSample(); ok {
		t.Fatal("readCPUSample ok = true with <5 fields, want false")
	}
}

func TestReadCPUSampleFailsWhenNotCPULine(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/stat": []byte("intr 1 2 3 4 5\n"),
	})
	if _, _, ok := readCPUSample(); ok {
		t.Fatal("readCPUSample ok = true when first token != cpu, want false")
	}
}

func TestCPUUsageZeroOnFirstSample(t *testing.T) {
	withProcFile(t, map[string][]byte{
		"/proc/stat": []byte("cpu  100 0 50 800 20 0 0 0 0 0\n"),
	})
	s := &procSampler{}
	if got := s.cpuUsage(); got != 0 {
		t.Fatalf("first cpuUsage = %v, want 0 (baseline)", got)
	}
}

func TestCPUUsageComputesBusyFractionAcrossSamples(t *testing.T) {
	old := osReadFile
	t.Cleanup(func() { osReadFile = old })
	current := []byte("cpu  100 0 50 800 20 0 0 0 0 0\n")
	osReadFile = func(string) ([]byte, error) { return current, nil }

	s := &procSampler{}
	if got := s.cpuUsage(); got != 0 {
		t.Fatalf("baseline cpuUsage = %v, want 0", got)
	}

	current = []byte("cpu  200 0 100 900 20 0 0 0 0 0\n")
	got := s.cpuUsage()
	if got <= 0 {
		t.Fatalf("second cpuUsage = %v, want > 0 for busy delta", got)
	}
	if got > 100 {
		t.Fatalf("second cpuUsage = %v, want <= 100", got)
	}
}

func TestCPUUsageZeroWhenReadFails(t *testing.T) {
	withProcFile(t, nil)
	s := &procSampler{}
	if got := s.cpuUsage(); got != 0 {
		t.Fatalf("cpuUsage on read error = %v, want 0", got)
	}
}

func TestCPUUsageZeroWhenIdleDeltaExceedsTotal(t *testing.T) {
	old := osReadFile
	t.Cleanup(func() { osReadFile = old })
	current := []byte("cpu  100 0 50 800 0 0 0 0 0 0\n")
	osReadFile = func(string) ([]byte, error) { return current, nil }

	s := &procSampler{}
	_ = s.cpuUsage()
	current = []byte("cpu  100 0 50 900 0 0 0 0 0 0\n")
	if got := s.cpuUsage(); got != 0 {
		t.Fatalf("cpuUsage = %v, want 0 when only idle advanced", got)
	}
}
