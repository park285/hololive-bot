package logging

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnableFileLogging_DoesNotCreateCombinedLog(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	cfg := Config{
		Level:      "info",
		Dir:        logDir,
		MaxSizeMB:  10,
		MaxBackups: 5,
		MaxAgeDays: 7,
	}

	if _, err := EnableFileLogging(cfg, "service.log"); err != nil {
		t.Fatalf("EnableFileLogging() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(logDir, "combined.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("combined.log stat error = %v, want %v", err, os.ErrNotExist)
	}
}

func TestArchiveCompressedLogFiles_MovesAndPrunesBackups(t *testing.T) {
	t.Parallel()

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
		t.Fatalf("archiveCompressedLogFiles() error = %v", err)
	}

	archiveDir := filepath.Join(logDir, archiveDirName)
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("read archive dir failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("archive entry count = %d, want 2", len(entries))
	}
}
