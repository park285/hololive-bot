package status

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	if FormatDuration(5*time.Minute) != "5m" {
		t.Fatal("minute formatting failed")
	}
	if FormatDuration(2*time.Hour) != "2h 0m" {
		t.Fatal("hour formatting failed")
	}
}

func TestMemoryStatsFromProc(t *testing.T) {
	old := osReadFile
	defer func() { osReadFile = old }()
	osReadFile = func(name string) ([]byte, error) {
		return []byte("MemTotal: 1000 kB\nMemAvailable: 250 kB\n"), nil
	}
	total, used := memoryStats()
	if total != 1024000 || used != 768000 {
		t.Fatalf("unexpected memory stats: %d %d", total, used)
	}
}
