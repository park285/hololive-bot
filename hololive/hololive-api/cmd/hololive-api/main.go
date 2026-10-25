package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/park285/shared-go/v2/pkg/health"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/telemetry"

	runtimeapp "github.com/kapu/hololive-api/internal/app"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/observability"
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

	os.Exit(bootstrap.Options[*apiconfig.RuntimeConfig, *observability.ManagedRuntime[*runtimeapp.Runtime]]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
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
	}, "hololive-api.log", sharedlogging.Options{OTel: config.Tracing.Enabled, AsyncStdout: true})
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
