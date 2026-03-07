package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"

	"github.com/kapu/hololive-kakao-bot-go/internal/app"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load_config_failed", slog.Any("error", err))
		return
	}

	logger, err := sharedlogging.EnableFileLoggingWithLevel(sharedlogging.Config{
		Dir:        cfg.Logging.Dir,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}, "warm_member_cache.log", cfg.Logging.Level)
	if err != nil {
		slog.Error("init_logger_failed", slog.Any("error", err))
		return
	}

	logger.Info("Manual member list cache refresh started")

	_, cleanup, err := app.InitializeWarmMemberCache(ctx, cfg, logger)
	if err != nil {
		logger.Error("Manual cache refresh failed", slog.Any("error", err))
		return
	}
	defer cleanup()

	logger.Info("Manual member list cache refresh completed successfully")
}
