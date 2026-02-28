package activity

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivityLogger_LogAndGetRecentLogs(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "activity.log")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	l := NewActivityLogger(filePath, logger)
	l.Log("command", "first", map[string]any{"key": "value"})
	l.Log("system", "second", nil)

	logs, err := l.GetRecentLogs(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	if logs[0].Summary != "first" || logs[1].Summary != "second" {
		t.Fatalf("unexpected log order: %+v", logs)
	}

	limited, err := l.GetRecentLogs(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(limited) != 1 || limited[0].Summary != "second" {
		t.Fatalf("unexpected limited logs: %+v", limited)
	}
}

func TestActivityLogger_GetRecentLogsMissingFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "missing.log")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	l := NewActivityLogger(filePath, logger)
	logs, err := l.GetRecentLogs(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected empty logs, got %d", len(logs))
	}
}

func TestActivityLogger_GetRecentLogsRingBufferLimit(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "activity.log")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	l := NewActivityLogger(filePath, logger)
	for i := 1; i <= 5; i++ {
		l.Log("command", fmt.Sprintf("entry-%d", i), nil)
	}

	logs, err := l.GetRecentLogs(3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}
	expected := []string{"entry-3", "entry-4", "entry-5"}
	for i, summary := range expected {
		if logs[i].Summary != summary {
			t.Fatalf("unexpected log order: %+v", logs)
		}
	}
}

func TestActivityLogger_LogRotateBySize(t *testing.T) {
	oldMaxBytes := activityLogRotateMaxBytes
	activityLogRotateMaxBytes = 256
	t.Cleanup(func() {
		activityLogRotateMaxBytes = oldMaxBytes
	})

	dir := t.TempDir()
	filePath := filepath.Join(dir, "activity.log")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	l := NewActivityLogger(filePath, logger)
	l.Log("system", strings.Repeat("x", 512), nil)
	l.Log("system", "after-rotate", nil)

	if _, err := os.Stat(filePath + ".1"); err != nil {
		t.Fatalf("expected rotated log file: %v", err)
	}

	logs, err := l.GetRecentLogs(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected current file to contain 1 log, got %d", len(logs))
	}
	if logs[0].Summary != "after-rotate" {
		t.Fatalf("unexpected recent logs after rotate: %+v", logs)
	}
}

func TestActivityLogger_StdoutOnlyMode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	l := NewActivityLogger("", logger)

	// stdoutOnly 모드에서 Log()는 패닉 없이 실행
	l.Log("command", "test-stdout", map[string]any{"key": "value"})

	// stdoutOnly 모드에서 GetRecentLogs()는 빈 슬라이스 반환
	logs, err := l.GetRecentLogs(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("expected empty logs in stdout mode, got %d", len(logs))
	}
}
