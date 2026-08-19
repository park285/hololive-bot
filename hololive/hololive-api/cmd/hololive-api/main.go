package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-api/internal/app"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/observability"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/pkg/telemetry"
)

var Version = "dev"

func main() {
	if handled, exitCode := runWorkerProfileCheck(os.Args[1:], os.Stderr, func() error {
		_, err := settings.LoadAPIWorkerProfile()
		return err
	}); handled {
		os.Exit(exitCode)
	}
	if handled, exitCode := runConfigCheck(os.Args[1:], os.Stderr, func() error {
		_, err := settings.LoadHololiveAPIRuntime()
		return err
	}); handled {
		os.Exit(exitCode)
	}

	var logCloser io.Closer
	code := bootstrap.Run(bootstrap.Options[*settings.HololiveAPIConfig, *observability.ManagedRuntime[*app.Runtime]]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		LoadConfig:             settings.LoadHololiveAPIRuntime,
		LoadConfigErrorMessage: "Failed to load hololive-api config",
		NewLogger: func(appConfig *settings.HololiveAPIConfig) (*slog.Logger, error) {
			logger, closer, err := sharedlogging.EnableFileLoggingWithOptions(sharedlogging.Config{
				Level:      appConfig.Logging.Level,
				Dir:        appConfig.Logging.Dir,
				MaxSizeMB:  appConfig.Logging.MaxSizeMB,
				MaxBackups: appConfig.Logging.MaxBackups,
				MaxAgeDays: appConfig.Logging.MaxAgeDays,
				Compress:   appConfig.Logging.Compress,
			}, "hololive-api.log", sharedlogging.Options{AsyncStdout: true})
			logCloser = closer
			if err == nil {
				slog.SetDefault(logger)
			}
			return logger, err
		},
		LoggerLevel: func(appConfig *settings.HololiveAPIConfig) string {
			return appConfig.Logging.Level
		},
		StartupMessage: "Hololive unified API starting...",
		StartupFields: func(appConfig *settings.HololiveAPIConfig) []any {
			return []any{
				slog.Int("bot_port", appConfig.Bot.Server.Port),
				slog.Int("admin_port", appConfig.Admin.Server.Port),
				slog.Int("llm_port", appConfig.LLM.Server.Port),
			}
		},
		BuildTimeout:      constants.AppTimeout.Build,
		BuildRuntime:      buildHololiveAPIRuntime,
		BuildErrorMessage: "Failed to assemble hololive-api runtime",
	})
	if logCloser != nil {
		if err := logCloser.Close(); err != nil {
			slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("log closer close failed", slog.Any("error", err))
		}
	}
	os.Exit(code)
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

func buildHololiveAPIRuntime(
	ctx context.Context,
	appConfig *settings.HololiveAPIConfig,
	logger *slog.Logger,
) (*observability.ManagedRuntime[*app.Runtime], error) {
	traceConfig := hololiveAPITelemetryConfig(appConfig, Version)
	return observability.BuildRuntime(ctx, &traceConfig, logger, func(ctx context.Context) (*app.Runtime, error) {
		return app.BuildRuntime(ctx, appConfig, logger)
	})
}

func hololiveAPITelemetryConfig(appConfig *settings.HololiveAPIConfig, version string) telemetry.Config {
	return telemetry.Config{
		Enabled:        appConfig.Tracing.Enabled,
		ServiceName:    "hololive-api",
		ServiceVersion: version,
		Environment:    appConfig.Bot.Environment,
		OTLPEndpoint:   appConfig.Tracing.Endpoint,
		OTLPInsecure:   appConfig.Tracing.Insecure,
		SampleRate:     appConfig.Tracing.SampleRate,
	}
}
