package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/park285/shared-go/v2/pkg/envutil"
	"github.com/park285/shared-go/v2/pkg/health"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/telemetry"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/observability"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
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

	startupTimeout, ok := parseStartupTimeout(os.Args[1:])
	if !ok {
		os.Exit(2)
	}

	os.Exit(bootstrap.Options[*collectorconfig.RuntimeConfig, *observability.ManagedRuntime[*collectorruntime.Runtime]]{
		Version: Version,
		Initialize: func(version string) {
			health.Init(version)
		},
		LoadConfig:             collectorconfig.LoadRuntime,
		LoadConfigErrorMessage: "Failed to load youtube collector config",
		NewLogger: func(appConfig *collectorconfig.RuntimeConfig) (*slog.Logger, io.Closer, error) {
			logger, closer, err := sharedlogging.EnableFileLoggingWithOptions(sharedlogging.Config{
				Level:      appConfig.Logging.Level,
				Dir:        appConfig.Logging.Dir,
				MaxSizeMB:  appConfig.Logging.MaxSizeMB,
				MaxBackups: appConfig.Logging.MaxBackups,
				MaxAgeDays: appConfig.Logging.MaxAgeDays,
				Compress:   appConfig.Logging.Compress,
			}, youtubeCollectorLogFileName(), sharedlogging.Options{OTel: appConfig.Tracing.Enabled, AsyncStdout: true, PrivacyKeys: privacylog.LoggerPrivacyKeys()})
			if err != nil {
				return nil, nil, fmt.Errorf("initialize correlated logger: %w", err)
			}

			// slog.Default()로 기록하는 의존성(otel export 오류 등)도 같은 JSON 파일과 마스킹 경로로 보낸다.
			slog.SetDefault(logger)

			return logger, closer, nil
		},
		LoggerLevel: func(appConfig *collectorconfig.RuntimeConfig) string {
			return appConfig.Logging.Level
		},
		StartupMessage: "YouTube Collector starting...",
		StartupFields: func(appConfig *collectorconfig.RuntimeConfig) []any {
			return []any{slog.Int("port", appConfig.Server.Port)}
		},
		BuildTimeout: startupTimeout,
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

// parseStartupTimeout은 native의 명시적 준비 예산만 허용하며 다른 실행 방식의 기본값은 유지합니다.
func parseStartupTimeout(args []string) (time.Duration, bool) {
	timeout := constants.AppTimeout.Build
	flags := flag.NewFlagSet("youtube-collector", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.DurationVar(&timeout, "startup-timeout", timeout, "runtime build timeout (positive, at most 90s)")

	if flags.Parse(args) != nil || flags.NArg() != 0 || timeout <= 0 || timeout > 90*time.Second {
		return 0, false
	}

	return timeout, true
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
