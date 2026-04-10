package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestOTelHandler_WithoutSpan(t *testing.T) {
	var buf bytes.Buffer
	baseHandler := slog.NewTextHandler(&buf, nil)
	handler := NewOTelHandler(baseHandler)

	logger := slog.New(handler)
	logger.Info("test message")

	output := buf.String()
	// trace_id/span_id가 없어야 함 (span 없는 context)
	if strings.Contains(output, "trace_id") {
		t.Errorf("expected no trace_id without span, got: %s", output)
	}
}

func TestOTelHandler_Enabled(t *testing.T) {
	baseHandler := slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := NewOTelHandler(baseHandler)

	// Info 레벨은 비활성화되어야 함
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected Info level to be disabled")
	}

	// Warn 레벨은 활성화되어야 함
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected Warn level to be enabled")
	}
}

func TestOTelHandler_WithAttrs(t *testing.T) {
	baseHandler := slog.NewTextHandler(nil, nil)
	handler := NewOTelHandler(baseHandler)

	newHandler := handler.WithAttrs([]slog.Attr{slog.String("key", "value")})
	if newHandler == nil {
		t.Fatal("WithAttrs returned nil")
	}
	_, ok := newHandler.(*OTelHandler)
	if !ok {
		t.Error("WithAttrs did not return OTelHandler")
	}
}

func TestOTelHandler_WithGroup(t *testing.T) {
	baseHandler := slog.NewTextHandler(nil, nil)
	handler := NewOTelHandler(baseHandler)

	newHandler := handler.WithGroup("testgroup")
	if newHandler == nil {
		t.Fatal("WithGroup returned nil")
	}
	_, ok := newHandler.(*OTelHandler)
	if !ok {
		t.Error("WithGroup did not return OTelHandler")
	}
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
