package util

import (
	"fmt"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
)

// LogConfig: 로깅 설정 (로그 디렉토리, 로테이션 정책)
type LogConfig = logging.Config

// NewLogger: 콘솔 출력용 기본 slog 로거를 생성합니다.
func NewLogger() *slog.Logger {
	return logging.NewLogger()
}

// NewLoggerWithLevel: 지정된 레벨로 콘솔 출력용 slog 로거를 생성합니다.
func NewLoggerWithLevel(level string) *slog.Logger {
	cfg := logging.Config{Level: level}
	logger, err := logging.EnableFileLoggingWithOTel(cfg, "", false)
	if err != nil || logger == nil {
		return logging.NewLogger()
	}
	return logger
}

// EnableFileLogging: 파일 로깅을 활성화하고, 로그 로테이션이 적용된 로거를 반환합니다.
func EnableFileLogging(cfg LogConfig, fileName string) (*slog.Logger, error) {
	logger, err := logging.EnableFileLogging(cfg, fileName)
	if err != nil {
		return nil, fmt.Errorf("enable file logging: %w", err)
	}
	return logger, nil
}

// EnableFileLoggingWithLevel: 지정된 레벨과 파일 로깅을 활성화합니다.
func EnableFileLoggingWithLevel(cfg LogConfig, fileName, level string) (*slog.Logger, error) {
	cfg.Level = level
	logger, err := logging.EnableFileLoggingWithOTel(cfg, fileName, false)
	if err != nil {
		return nil, fmt.Errorf("enable file logging with level: %w", err)
	}
	return logger, nil
}
