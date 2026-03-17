package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig_LoadsLoggingEnv(t *testing.T) {
	t.Setenv("LOG_DIR", "/tmp/settlement-logs")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_MAX_SIZE_MB", "17")
	t.Setenv("LOG_MAX_BACKUPS", "9")
	t.Setenv("LOG_MAX_AGE_DAYS", "21")
	t.Setenv("LOG_COMPRESS", "false")

	cfg := loadConfig()

	if cfg.logDir != "/tmp/settlement-logs" {
		t.Fatalf("logDir = %q, want %q", cfg.logDir, "/tmp/settlement-logs")
	}
	if cfg.logLevel != "debug" {
		t.Fatalf("logLevel = %q, want %q", cfg.logLevel, "debug")
	}
	if cfg.logMaxSizeMB != 17 {
		t.Fatalf("logMaxSizeMB = %d, want 17", cfg.logMaxSizeMB)
	}
	if cfg.logMaxBackups != 9 {
		t.Fatalf("logMaxBackups = %d, want 9", cfg.logMaxBackups)
	}
	if cfg.logMaxAgeDays != 21 {
		t.Fatalf("logMaxAgeDays = %d, want 21", cfg.logMaxAgeDays)
	}
	if cfg.logCompress {
		t.Fatal("logCompress = true, want false")
	}
}

func TestNewLogger_CreatesSettlementLogFile(t *testing.T) {
	dir := t.TempDir()
	cfg := appConfig{
		logDir:        dir,
		logLevel:      "info",
		logMaxSizeMB:  10,
		logMaxBackups: 3,
		logMaxAgeDays: 7,
		logCompress:   false,
	}

	logger, err := newLogger(cfg)
	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}

	logger.Info("settlement-log-file-test")

	logPath := filepath.Join(dir, "settlement-bot.log")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(logPath)
		if readErr == nil && strings.Contains(string(data), "settlement-log-file-test") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", logPath, err)
	}
	if !strings.Contains(string(data), "settlement-log-file-test") {
		t.Fatalf("log file %q does not contain settlement test marker", logPath)
	}
}
