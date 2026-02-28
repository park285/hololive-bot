package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/automaxprocs"

	"github.com/kapu/hololive-alarm/internal/app"
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

	cfg, err := config.LoadAlarmDispatcher()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load alarm dispatcher config: %v\n", err)
		exitCode = 1
		return
	}

	// slog 기반 로거 초기화 (파일 로깅 포함)
	logger, err := util.EnableFileLoggingWithLevel(util.LogConfig{
		Dir:        cfg.Logging.Dir,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}, "alarm-dispatcher.log", cfg.Logging.Level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		exitCode = 1
		return
	}

	logger.Info("Alarm Dispatcher starting...",
		slog.String("version", Version),
		slog.String("log_level", cfg.Logging.Level),
		slog.Int("port", cfg.Port),
	)

	buildCtx, buildCancel := context.WithTimeout(context.Background(), 1*time.Minute)
	runtime, err := app.BuildAlarmDispatcherRuntime(buildCtx, cfg, logger)
	buildCancel()
	if err != nil {
		logger.Error("Failed to build alarm dispatcher runtime", slog.Any("error", err))
		exitCode = 1
		return
	}
	defer runtime.Close()

	runtime.Run()
}
