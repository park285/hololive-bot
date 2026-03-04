package logging_test

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/logging"
)

// TestNewLogger: 기본 로거 생성이 nil이 아닌 *slog.Logger를 반환하는지 검증합니다.
func TestNewLogger(t *testing.T) {
	t.Parallel()
	logger := logging.NewLogger()
	if logger == nil {
		t.Fatal("NewLogger()가 nil을 반환했습니다")
	}
	if _, ok := any(logger).(*slog.Logger); !ok {
		t.Fatal("NewLogger()가 *slog.Logger를 반환하지 않았습니다")
	}
}

// TestNewLoggerWithLevel: 지정된 레벨로 로거를 생성하고 nil이 아닌지 검증합니다.
func TestNewLoggerWithLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		level string
	}{
		{"debug 레벨", "debug"},
		{"info 레벨", "info"},
		{"잘못된 레벨은 기본값으로 폴백", "invalid_level"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger := logging.NewLoggerWithLevel(tt.level)
			if logger == nil {
				t.Errorf("NewLoggerWithLevel(%q)가 nil을 반환했습니다", tt.level)
			}
		})
	}
}

// TestEnableFileLogging: 파일 로깅 활성화 성공/실패 케이스를 검증합니다.
func TestEnableFileLogging(t *testing.T) {
	t.Parallel()

	t.Run("유효한 디렉토리는 성공", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		cfg := logging.Config{
			Dir:        tmpDir,
			MaxSizeMB:  10,
			MaxBackups: 5,
			MaxAgeDays: 7,
		}
		logger, err := logging.EnableFileLogging(cfg, "test.log")
		if err != nil {
			t.Fatalf("EnableFileLogging 오류 발생: %v", err)
		}
		if logger == nil {
			t.Fatal("EnableFileLogging()가 nil 로거를 반환했습니다")
		}
	})

	t.Run("생성 불가능한 경로는 오류 반환", func(t *testing.T) {
		t.Parallel()
		cfg := logging.Config{
			Dir:        "/proc/nonexistent/deeply/nested/dir",
			MaxSizeMB:  10,
			MaxBackups: 5,
			MaxAgeDays: 7,
		}
		_, err := logging.EnableFileLogging(cfg, "test.log")
		if err == nil {
			t.Fatal("EnableFileLogging이 오류를 반환해야 하지만 nil을 반환했습니다")
		}
	})
}
