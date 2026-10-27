package main

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/health"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/telemetry"

	runtimeapp "github.com/kapu/hololive-api/internal/app"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/llm/notification"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/observability"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

var Version = "dev"

func main() {
	if handled, exitCode := runWorkerProfileCheck(os.Args[1:], os.Stderr, func() error {
		if _, err := apiconfig.LoadAPIWorkerProfile(); err != nil {
			return fmt.Errorf("load api worker profile: %w", err)
		}

		return nil
	}); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := runConfigCheck(os.Args[1:], os.Stderr, func() error {
		if _, err := apiconfig.LoadRuntime(); err != nil {
			return fmt.Errorf("load hololive api runtime: %w", err)
		}

		return nil
	}); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := runNotificationRerun(os.Args[1:], os.Stdout, os.Stderr, rerunNotification); handled {
		os.Exit(exitCode)
	}

	os.Exit(bootstrap.Options[*apiconfig.RuntimeConfig, *observability.ManagedRuntime[*runtimeapp.Runtime]]{
		Version: Version,
		Initialize: func(version string) {
			health.Init(version)
		},
		LoadConfig:             apiconfig.LoadRuntime,
		LoadConfigErrorMessage: "Failed to load hololive-api config",
		NewLogger:              newHololiveAPILogger,
		LoggerLevel: func(config *apiconfig.RuntimeConfig) string {
			return config.Logging.Level
		},
		StartupMessage: "Hololive unified API starting...",
		StartupFields: func(config *apiconfig.RuntimeConfig) []any {
			return []any{
				slog.Int("bot_port", config.Bot.Server.Port),
				slog.Int("admin_port", config.Admin.Server.Port),
				slog.Int("llm_port", config.LLM.Server.Port),
			}
		},
		BuildTimeout: constants.AppTimeout.Build,
		BuildRuntime: func(
			ctx context.Context,
			config *apiconfig.RuntimeConfig,
			logger *slog.Logger,
		) (*observability.ManagedRuntime[*runtimeapp.Runtime], error) {
			traceConfig := hololiveAPITelemetryConfig(config, Version)

			return observability.BuildRuntime(
				ctx,
				&traceConfig,
				logger,
				func(ctx context.Context) (*runtimeapp.Runtime, error) {
					return runtimeapp.BuildRuntime(ctx, config, logger)
				},
			)
		},
		BuildErrorMessage: "Failed to assemble hololive-api runtime",
	}.Run())
}

func newHololiveAPILogger(config *apiconfig.RuntimeConfig) (*slog.Logger, io.Closer, error) {
	logger, closer, err := sharedlogging.EnableFileLoggingWithOptions(sharedlogging.Config{
		Level:      config.Logging.Level,
		Dir:        config.Logging.Dir,
		MaxSizeMB:  config.Logging.MaxSizeMB,
		MaxBackups: config.Logging.MaxBackups,
		MaxAgeDays: config.Logging.MaxAgeDays,
		Compress:   config.Logging.Compress,
	}, "hololive-api.log", sharedlogging.Options{OTel: config.Tracing.Enabled, AsyncStdout: true, PrivacyKeys: privacylog.LoggerPrivacyKeys()})
	if err != nil {
		return nil, nil, fmt.Errorf("enable file logging: %w", err)
	}

	// slog.Default()로 기록하는 의존성(otel export 오류 등)도 같은 JSON 파일과 마스킹 경로로 보낸다.
	slog.SetDefault(logger)

	return logger, closer, nil
}

func hololiveAPITelemetryConfig(config *apiconfig.RuntimeConfig, version string) telemetry.Config {
	return telemetry.Config{
		Enabled:        config.Tracing.Enabled,
		ServiceName:    "hololive-api",
		ServiceVersion: version,
		Environment:    config.Bot.Environment,
		OTLPEndpoint:   config.Tracing.Endpoint,
		OTLPInsecure:   config.Tracing.Insecure,
		SampleRate:     config.Tracing.SampleRate,
	}
}

func runWorkerProfileCheck(args []string, stderr io.Writer, load func() error) (handled bool, exitCode int) {
	if len(args) != 1 || args[0] != "--check-worker-profile" {
		return false, 0
	}

	if err := load(); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Failed to load hololive-api worker profile: %v\n", err); writeErr != nil {
			return true, 1
		}

		return true, 1
	}

	if _, err := fmt.Fprintln(stderr, "hololive-api worker profile valid"); err != nil {
		return true, 1
	}

	return true, 0
}

func runConfigCheck(args []string, stderr io.Writer, load func() error) (handled bool, exitCode int) {
	if len(args) != 1 || args[0] != "--check-config" {
		return false, 0
	}

	if err := load(); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Failed to load hololive-api config: %v\n", err); writeErr != nil {
			return true, 1
		}

		return true, 1
	}

	if _, err := fmt.Fprintln(stderr, "hololive-api config valid"); err != nil {
		return true, 1
	}

	return true, 0
}

const rerunNotificationFlag = "--rerun-notification="

// notificationRerunTimeout은 수동 재실행 한 번의 상한이다. 방별 LLM 요약과 outbox 적재가 중간에 끊기면 일부 방만
// 적재되므로 signal로 취소하지 않고 이 시한으로만 끊는다(정기 실행 trigger와 같은 5분).
const notificationRerunTimeout = 5 * time.Minute

// runNotificationRerun은 `hololive-api --rerun-notification=<kind>` 운영 명령이다. 결과 JSON을 stdout에 쓴다.
// exit 0은 오류 없이 끝났다는 뜻이며 일부 방 실패는 결과의 status(partial)와 failed로 보인다. 같은 주·월 알림이 이미
// 실행 중이면 exit 2, 그 밖의 실패는 exit 1이다.
func runNotificationRerun(args []string, stdout, stderr io.Writer, run func(context.Context, string) (notification.Result, error)) (handled bool, exitCode int) {
	if len(args) != 1 {
		return false, 0
	}

	kind, ok := strings.CutPrefix(args[0], rerunNotificationFlag)
	if !ok {
		return false, 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), notificationRerunTimeout)
	defer cancel()

	result, err := run(ctx, kind)
	if err != nil {
		code := 1
		if errors.Is(err, notification.ErrNotificationInProgress) {
			code = 2
		}

		if _, writeErr := fmt.Fprintf(stderr, "hololive-api notification rerun failed: %v\n", err); writeErr != nil {
			return true, 1
		}

		return true, code
	}

	if err := jsonv2.MarshalWrite(stdout, result); err != nil {
		return true, 1
	}

	if _, err := fmt.Fprintln(stdout); err != nil {
		return true, 1
	}

	return true, 0
}

func rerunNotification(ctx context.Context, kind string) (notification.Result, error) {
	config, err := apiconfig.LoadRuntime()
	if err != nil {
		return notification.Result{}, fmt.Errorf("load hololive api runtime: %w", err)
	}

	logger, err := newRerunNotificationLogger(config)
	if err != nil {
		return notification.Result{}, err
	}

	result, err := runtimeapp.RerunNotification(ctx, config, kind, logger)
	if err != nil {
		return result, fmt.Errorf("rerun notification: %w", err)
	}

	return result, nil
}

// newRerunNotificationLogger는 운영 CLI 전용 logger다. stdout에는 Result JSON 한 줄만 남아야 하고, CLI는 실행 중인
// 서버와 같은 container에서 돌므로 서버의 회전 로그 파일과 archiver를 함께 건드리면 안 된다. 그래서 파일 lane 없는
// 콘솔 구성(Dir 빈 값, Closer nil)으로만 만든다. shared-go 콘솔 handler는 os.Stdout으로 쓰므로, main이 결과 writer를
// 잡아 둔 뒤 여기서 os.Stdout을 stderr로 돌린다. 라이브러리가 stdout에 직접 쓰는 출력도 함께 stderr로 간다.
func newRerunNotificationLogger(config *apiconfig.RuntimeConfig) (*slog.Logger, error) {
	os.Stdout = os.Stderr

	logger, _, err := sharedlogging.EnableFileLoggingWithOptions(sharedlogging.Config{
		Level: config.Logging.Level,
	}, "hololive-api.log", sharedlogging.Options{OTel: false, PrivacyKeys: privacylog.LoggerPrivacyKeys()})
	if err != nil {
		return nil, fmt.Errorf("enable console logging: %w", err)
	}

	slog.SetDefault(logger)

	return logger, nil
}
