package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/automaxprocs"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"

	"github.com/kapu/hololive-stream-ingester/internal/app"
)

// Version: 빌드 시 ldflags로 주입됨 (예: -ldflags="-X main.Version=1.0.0")
var Version = "dev"

func main() {
	automaxprocs.Init(nil)
	health.Init(Version)

	var exitCode int
	defer func() {
		os.Exit(exitCode)
	}()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load stream ingester config: %v\n", err)
		exitCode = 1
		return
	}

	logger, err := sharedlogging.EnableFileLoggingWithLevel(sharedlogging.Config{
		Dir:        cfg.Logging.Dir,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}, "stream-ingester.log", cfg.Logging.Level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		exitCode = 1
		return
	}

	logger.Info("Stream Ingester starting...",
		slog.String("version", Version),
		slog.String("log_level", cfg.Logging.Level),
		slog.Int("port", cfg.Server.Port),
	)

	buildCtx, buildCancel := context.WithTimeout(context.Background(), 1*time.Minute)
	runtime, err := app.BuildStreamIngesterRuntime(buildCtx, cfg, logger)
	buildCancel()
	if err != nil {
		logger.Error("Failed to build stream ingester runtime", slog.Any("error", err))
		exitCode = 1
		return
	}
	defer runtime.Close()

	runtime.Run()
}
