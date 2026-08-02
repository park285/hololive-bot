// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package activity

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivityLogger_LogAndGetRecentLogs(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "activity.log")
	logger := slog.New(slog.DiscardHandler)

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
	logger := slog.New(slog.DiscardHandler)

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
	logger := slog.New(slog.DiscardHandler)

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
	logger := slog.New(slog.DiscardHandler)

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
	logger := slog.New(slog.DiscardHandler)

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

func TestActivityLogger_GetRecentLogsTailAcrossChunkBoundary(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "activity.log")
	logger := slog.New(slog.DiscardHandler)

	l := NewActivityLogger(filePath, logger)
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Errorf("close activity logger: %v", err)
		}
	})

	// 한 줄이 1KB를 넘도록 만들어 tail chunk(64KB) 경계를 여러 번 가로지르게 한다.
	const entries = 200
	padding := strings.Repeat("p", 1024)
	for i := range entries {
		l.Log("system", fmt.Sprintf("entry-%d", i), map[string]any{"pad": padding})
	}

	logs, err := l.GetRecentLogs(3)
	if err != nil {
		t.Fatalf("GetRecentLogs() error = %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("GetRecentLogs() len = %d, want 3", len(logs))
	}

	want := []string{"entry-197", "entry-198", "entry-199"}
	for i, summary := range want {
		if logs[i].Summary != summary {
			t.Fatalf("logs[%d].Summary = %q, want %q (full order: %v)", i, logs[i].Summary, summary, logSummaries(logs))
		}
	}
	if logs[2].Details["pad"] != padding {
		t.Fatal("tail read lost entry details")
	}
}

func TestActivityLogger_GetRecentLogsHonoursLimitLargerThanFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "activity.log")
	l := NewActivityLogger(filePath, slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Errorf("close activity logger: %v", err)
		}
	})

	l.Log("system", "only-entry", nil)

	logs, err := l.GetRecentLogs(50)
	if err != nil {
		t.Fatalf("GetRecentLogs() error = %v", err)
	}
	if len(logs) != 1 || logs[0].Summary != "only-entry" {
		t.Fatalf("GetRecentLogs() = %v, want a single only-entry", logSummaries(logs))
	}
}

func TestActivityLogger_GetRecentLogsSkipsCorruptTrailingLine(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "activity.log")

	content := `{"timestamp":"2026-01-01T00:00:00Z","type":"system","summary":"good-1"}` + "\n" +
		`{"timestamp":"2026-01-01T00:00:01Z","type":"system","summary":"good-2"}` + "\n" +
		"{not-json\n"
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write activity log fixture: %v", err)
	}

	l := NewActivityLogger(filePath, slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Errorf("close activity logger: %v", err)
		}
	})

	logs, err := l.GetRecentLogs(2)
	if err != nil {
		t.Fatalf("GetRecentLogs() error = %v", err)
	}
	if len(logs) != 2 || logs[0].Summary != "good-1" || logs[1].Summary != "good-2" {
		t.Fatalf("GetRecentLogs() = %v, want [good-1 good-2]", logSummaries(logs))
	}
}

func TestActivityLogger_LogReopensAfterExternalRemoval(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "activity.log")
	l := NewActivityLogger(filePath, slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Errorf("close activity logger: %v", err)
		}
	})

	l.Log("system", "before-removal", nil)
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("remove activity log: %v", err)
	}

	l.Log("system", "after-removal", nil)

	logs, err := l.GetRecentLogs(10)
	if err != nil {
		t.Fatalf("GetRecentLogs() error = %v", err)
	}
	if len(logs) != 1 || logs[0].Summary != "after-removal" {
		t.Fatalf("GetRecentLogs() = %v, want [after-removal]", logSummaries(logs))
	}
}

func logSummaries(logs []LogEntry) []string {
	summaries := make([]string, 0, len(logs))
	for _, entry := range logs {
		summaries = append(summaries, entry.Summary)
	}
	return summaries
}
