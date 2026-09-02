package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	_ "time/tzdata"

	"github.com/park285/shared-go/v2/pkg/envutil"
	"github.com/park285/shared-go/v2/pkg/health"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/telemetry"

	collectorconfig "github.com/kapu/hololive-shared/pkg/config/settings/collector"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/observability"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectorruntime"
)

var (
	Version  = "dev"
	Revision = "unknown"
)

func main() {
	if handled, exitCode := runWorkerProfileCheck(os.Args[1:], os.Stderr, func() error {
		if _, err := collectorconfig.LoadWorkerProfile(); err != nil {
			return fmt.Errorf("load youtube collector worker profile: %w", err)
		}

		return nil
	}); handled {
		os.Exit(exitCode)
	}

	os.Exit(bootstrap.Options[*collectorconfig.RuntimeConfig, *observability.ManagedRuntime[*collectorruntime.Runtime]]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		LoadConfig:             collectorconfig.LoadRuntime,
		LoadConfigErrorMessage: "Failed to load youtube collector config",
		LoggerConfig: func(appConfig *collectorconfig.RuntimeConfig) sharedlogging.Config {
			return sharedlogging.Config{
				Dir:        appConfig.Logging.Dir,
				MaxSizeMB:  appConfig.Logging.MaxSizeMB,
				MaxBackups: appConfig.Logging.MaxBackups,
				MaxAgeDays: appConfig.Logging.MaxAgeDays,
				Compress:   appConfig.Logging.Compress,
			}
		},
		LoggerFileName: youtubeCollectorLogFileName(),
		LoggerLevel: func(appConfig *collectorconfig.RuntimeConfig) string {
			return appConfig.Logging.Level
		},
		StartupMessage: "YouTube Collector starting...",
		StartupFields: func(appConfig *collectorconfig.RuntimeConfig) []any {
			return []any{slog.Int("port", appConfig.Server.Port)}
		},
		BuildTimeout: constants.AppTimeout.Build,
		BuildRuntime: func(
			ctx context.Context,
			appConfig *collectorconfig.RuntimeConfig,
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
	}.Run())
}

func runWorkerProfileCheck(args []string, stderr io.Writer, load func() error) (handled bool, exitCode int) {
	if len(args) != 1 || args[0] != "--check-worker-profile" {
		return false, 0
	}

	if err := load(); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Failed to load youtube collector worker profile: %v\n", err); writeErr != nil {
			return true, 1
		}

		return true, 1
	}

	if _, err := fmt.Fprintln(stderr, "youtube collector worker profile valid"); err != nil {
		return true, 1
	}

	return true, 0
}

func youtubeCollectorTelemetryConfig(appConfig *collectorconfig.RuntimeConfig, version string) telemetry.Config {
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
