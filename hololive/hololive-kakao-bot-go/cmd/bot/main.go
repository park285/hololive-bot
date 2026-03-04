package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/automaxprocs"

	"github.com/kapu/hololive-kakao-bot-go/internal/app"
	"github.com/kapu/hololive-kakao-bot-go/internal/telemetry"
)

// Version: 빌드 시 ldflags로 주입됨 (예: -ldflags="-X main.Version=1.0.0")
var Version = "dev"

func main() {
	// automaxprocs: 컨테이너 환경에서 CPU 할당량에 맞춰 GOMAXPROCS 자동 설정
	automaxprocs.Init(nil)

	// health 패키지 초기화 (버전/uptime 추적)
	health.Init(Version)

	// Graceful Shutdown을 위해 os.Exit 대신 exitCode 변수 사용
	var exitCode int
	defer func() {
		os.Exit(exitCode)
	}()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		exitCode = 1
		return
	}

	// slog 기반 로거 초기화 (파일 로깅 포함)

	logger, err := sharedlogging.EnableFileLoggingWithLevel(sharedlogging.Config{
		Dir:        cfg.Logging.Dir,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}, "bot.log", cfg.Logging.Level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		exitCode = 1
		return
	}

	// OpenTelemetry Provider 초기화
	otelProvider, err := telemetry.NewProvider(context.Background(), telemetry.Config{
		Enabled:        cfg.Telemetry.Enabled,
		ServiceName:    cfg.Telemetry.ServiceName,
		ServiceVersion: cfg.Telemetry.ServiceVersion,
		Environment:    cfg.Telemetry.Environment,
		OTLPEndpoint:   cfg.Telemetry.OTLPEndpoint,
		OTLPInsecure:   cfg.Telemetry.OTLPInsecure,
		SampleRate:     cfg.Telemetry.SampleRate,
	})
	if err != nil {
		logger.Error("otel_init_failed", slog.Any("err", err))
		exitCode = 1
		return
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := otelProvider.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("otel_shutdown_failed", slog.Any("err", shutdownErr))
		}
	}()

	if otelProvider.IsEnabled() {
		logger.Info("otel_enabled",
			slog.String("service", cfg.Telemetry.ServiceName),
			slog.String("endpoint", cfg.Telemetry.OTLPEndpoint),
			slog.Float64("sample_rate", cfg.Telemetry.SampleRate),
		)
	}

	logger.Info("Hololive KakaoTalk Bot starting...",
		slog.String("version", Version),
		slog.String("log_level", cfg.Logging.Level),
	)

	buildCtx, buildCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Build)
	runtime, err := app.BuildRuntime(buildCtx, cfg, logger)
	buildCancel()
	if err != nil {
		logger.Error("Failed to assemble application services", slog.Any("error", err))
		exitCode = 1
		return
	}
	defer runtime.Close()

	runtime.Run()
}
