package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	_ "time/tzdata"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/observability"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectorruntime"
	"github.com/park285/shared-go/pkg/envutil"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/pkg/telemetry"
)

var Version = "dev"
var Revision = "unknown"

func main() {
	os.Exit(bootstrap.Run(bootstrap.Options[*settings.YouTubeCollectorRuntimeConfig, *observability.ManagedRuntime[*collectorruntime.Runtime]]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		LoadConfig:             settings.LoadYouTubeCollectorRuntime,
		LoadConfigErrorMessage: "Failed to load youtube collector config",
		LoggerConfig: func(appConfig *settings.YouTubeCollectorRuntimeConfig) sharedlogging.Config {
			return sharedlogging.Config{
				Dir:        appConfig.Logging.Dir,
				MaxSizeMB:  appConfig.Logging.MaxSizeMB,
				MaxBackups: appConfig.Logging.MaxBackups,
				MaxAgeDays: appConfig.Logging.MaxAgeDays,
				Compress:   appConfig.Logging.Compress,
			}
		},
		LoggerFileName: youtubeCollectorLogFileName(),
		LoggerLevel: func(appConfig *settings.YouTubeCollectorRuntimeConfig) string {
			return appConfig.Logging.Level
		},
		StartupMessage: "YouTube Collector starting...",
		StartupFields: func(appConfig *settings.YouTubeCollectorRuntimeConfig) []any {
			return []any{slog.Int("port", appConfig.Server.Port)}
		},
		BuildTimeout: constants.AppTimeout.Build,
		BuildRuntime: func(
			ctx context.Context,
			appConfig *settings.YouTubeCollectorRuntimeConfig,
			logger *slog.Logger,
		) (*observability.ManagedRuntime[*collectorruntime.Runtime], error) {
			traceConfig := youtubeCollectorTelemetryConfig(appConfig, Version)
			return observability.BuildRuntime(
				ctx,
				&traceConfig,
				logger,
				func(ctx context.Context) (*collectorruntime.Runtime, error) {
					return collectorruntime.Build(ctx, appConfig, logger)
				},
			)
		},
		BuildErrorMessage: "Failed to build youtube collector runtime",
	}))
}

func youtubeCollectorTelemetryConfig(appConfig *settings.YouTubeCollectorRuntimeConfig, version string) telemetry.Config {
	return telemetry.Config{
		Enabled:        appConfig.Tracing.Enabled,
		ServiceName:    "youtube-collector",
		ServiceVersion: version,
		Environment:    appConfig.Environment,
		OTLPEndpoint:   appConfig.Tracing.Endpoint,
		OTLPInsecure:   appConfig.Tracing.Insecure,
		SampleRate:     appConfig.Tracing.SampleRate,
	}
}

func youtubeCollectorLogFileName() string {
	fileName := envutil.String("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "")
	if fileName == "" || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		return "youtube-collector.log"
	}
	return fileName
}
