package main

import (
	"context"
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
		BuildTimeout: constants.AppTimeout.Build,
		BuildRuntime: func(
			ctx context.Context,
			appConfig *settings.HololiveAPIConfig,
			logger *slog.Logger,
		) (*observability.ManagedRuntime[*app.Runtime], error) {
			traceConfig := hololiveAPITelemetryConfig(appConfig, Version)
			return observability.BuildRuntime(
				ctx,
				&traceConfig,
				logger,
				func(ctx context.Context) (*app.Runtime, error) {
					return app.BuildRuntime(ctx, appConfig, logger)
				},
			)
		},
		BuildErrorMessage: "Failed to assemble hololive-api runtime",
	})
	if logCloser != nil {
		if err := logCloser.Close(); err != nil {
			slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("log closer close failed", slog.Any("error", err))
		}
	}
	os.Exit(code)
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
