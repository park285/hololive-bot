package bootstrap

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/logging"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/config"
)

func NewLogger() *slog.Logger {
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: time.RFC3339,
		AddSource:  true,
	}))
}

func NewLoggerWithConfig(cfg config.LogConfig, enableOTel bool) (*slog.Logger, error) {
	logger, err := logging.EnableFileLoggingWithOTel(cfg, "admin.log", enableOTel)
	if err != nil {
		return nil, fmt.Errorf("enable file logging: %w", err)
	}
	return logger, nil
}
