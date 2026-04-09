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

package logging

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger()
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
}

func TestNewTestLogger(t *testing.T) {
	logger := NewTestLogger()
	if logger == nil {
		t.Fatal("NewTestLogger returned nil")
	}

	logger.Info("test message", "key", "value")
	logger.Error("error message", "error", "test error")
}

func TestNewTestLoggerWithOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := NewTestLoggerWithOutput(&buf)
	if logger == nil {
		t.Fatal("NewTestLoggerWithOutput returned nil")
	}

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("expected log output to contain 'test message', got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected log output to contain 'key=value', got: %s", output)
	}
}

func TestNewTestLoggerDiscardsOutput(t *testing.T) {
	logger := NewTestLogger()

	logger.Info("this should be discarded")
	logger.Error("this should also be discarded")
}

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "empty dir returns nil",
			cfg:     Config{Dir: "", MaxSizeMB: 10, MaxBackups: 5, MaxAgeDays: 7},
			wantErr: false,
		},
		{
			name:    "invalid size",
			cfg:     Config{Dir: "/tmp", MaxSizeMB: 0, MaxBackups: 5, MaxAgeDays: 7},
			wantErr: true,
		},
		{
			name:    "invalid backups",
			cfg:     Config{Dir: "/tmp", MaxSizeMB: 10, MaxBackups: 0, MaxAgeDays: 7},
			wantErr: true,
		},
		{
			name:    "invalid age",
			cfg:     Config{Dir: "/tmp", MaxSizeMB: 10, MaxBackups: 5, MaxAgeDays: 0},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EnableFileLogging(tt.cfg, "test.log")
			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestEnableFileLogging_EnsuresReadableFilePerms(t *testing.T) {
	logDir := t.TempDir()
	serviceLogPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(serviceLogPath, []byte("preexisting\n"), 0o600); err != nil {
		t.Fatalf("write preexisting log failed: %v", err)
	}

	cfg := Config{
		Level:      "info",
		Dir:        logDir,
		MaxSizeMB:  10,
		MaxBackups: 5,
		MaxAgeDays: 7,
	}

	if _, err := EnableFileLogging(cfg, "service.log"); err != nil {
		t.Fatalf("EnableFileLogging failed: %v", err)
	}

	tests := []string{
		serviceLogPath,
	}

	for _, path := range tests {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s failed: %v", path, err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("unexpected perm for %s: got %o want %o", path, got, 0o644)
		}
	}
}

func TestShouldDisableColor_NonFileWriter(t *testing.T) {
	if !shouldDisableColor(&bytes.Buffer{}) {
		t.Fatal("expected non-file writer to disable color")
	}
}

func TestShouldDisableColor_WithNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if !shouldDisableColor(os.Stdout) {
		t.Fatal("expected NO_COLOR env to disable color")
	}
}

func TestArchiveCompressedLogFiles_MovesAndPrunesBackups(t *testing.T) {
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "service.log")
	if err := os.WriteFile(logPath, []byte("active\n"), 0o644); err != nil {
		t.Fatalf("write active log failed: %v", err)
	}

	now := time.Now().UTC()
	names := []string{
		"service-" + now.Add(-48*time.Hour).Format(backupTimeFormat) + ".log.gz",
		"service-" + now.Add(-24*time.Hour).Format(backupTimeFormat) + ".log.gz",
		"service-" + now.Add(-(31*24)*time.Hour).Format(backupTimeFormat) + ".log.gz",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write compressed backup failed: %v", err)
		}
	}

	if err := archiveCompressedLogFiles(logPath, 2, 30); err != nil {
		t.Fatalf("archiveCompressedLogFiles failed: %v", err)
	}

	archiveDir := filepath.Join(logDir, archiveDirName)
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("read archive dir failed: %v", err)
	}

	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	slices.Sort(got)

	want := []string{
		"service-" + now.Add(-48*time.Hour).Format(backupTimeFormat) + ".log.gz",
		"service-" + now.Add(-24*time.Hour).Format(backupTimeFormat) + ".log.gz",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("archived backups = %v, want %v", got, want)
	}

	for _, name := range names {
		if _, err := os.Stat(filepath.Join(logDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected source backup %s to be moved or removed, err=%v", name, err)
		}
	}
}
