package main

import (
	"context"
	"errors"
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
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/entrypoint"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectorruntime"
)

var Version = "dev"

func main() {
	if handled, exitCode := entrypoint.RunWorkerProfileCheck(os.Args[1:], os.Stderr, "youtube collector", func() error {
		if _, err := collectorconfig.LoadWorkerProfile(); err != nil {
			return fmt.Errorf("load youtube collector worker profile: %w", err)
		}

		return nil
	}); handled {
		os.Exit(exitCode)
	}

	startup, ok := parseStartupOptions(os.Args[1:])
	if !ok {
		os.Exit(2)
	}

	os.Exit(bootstrap.Options[*collectorconfig.RuntimeConfig, *collectorruntime.Runtime]{
		Version: Version,
		Initialize: func(version string) {
			health.Init(version)
		},
		LoadConfig:             collectorconfig.LoadRuntime,
		LoadConfigErrorMessage: "Failed to load youtube collector config",
		NewLogger: func(appConfig *collectorconfig.RuntimeConfig) (*slog.Logger, io.Closer, error) {
			logFileName, err := youtubeCollectorLogFileName()
			if err != nil {
				return nil, nil, fmt.Errorf("resolve log file name: %w", err)
			}

			return entrypoint.NewFileLogger(appConfig.Logging, logFileName)
		},
		LoggerLevel: func(appConfig *collectorconfig.RuntimeConfig) string {
			return appConfig.Logging.Level
		},
		StartupMessage: "YouTube Collector starting...",
		StartupFields: func(appConfig *collectorconfig.RuntimeConfig) []any {
			return []any{slog.Int("port", appConfig.Server.Port)}
		},
		BuildTimeout: startup.timeout,
		BuildRuntime: func(
			ctx context.Context,
			appConfig *collectorconfig.RuntimeConfig,
			logger *slog.Logger,
		) (*collectorruntime.Runtime, error) {
			return collectorruntime.Build(ctx, appConfig, logger, startup.waitForDatabase)
		},
		BuildErrorMessage: "Failed to build youtube collector runtime",
	}.Run())
}

type startupOptions struct {
	timeout         time.Duration
	waitForDatabase bool
}

func parseStartupOptions(args []string) (startupOptions, bool) {
	options := startupOptions{timeout: constants.AppTimeout.Build}
	flags := flag.NewFlagSet("youtube-collector", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.DurationVar(&options.timeout, "startup-timeout", options.timeout, "runtime build timeout (positive, at most 90s)")
	flags.BoolVar(&options.waitForDatabase, "wait-for-database", false, "wait for initial database connection deadlines within the startup budget")

	if flags.Parse(args) != nil || flags.NArg() != 0 || options.timeout <= 0 || options.timeout > 90*time.Second {
		return startupOptions{}, false
	}

	return options, true
}

func youtubeCollectorLogFileName() (string, error) {
	fileName := envutil.String("YOUTUBE_COLLECTOR_LOG_FILE_NAME", "")
	if fileName == "" {
		return "youtube-collector.log", nil
	}

	if strings.ContainsAny(fileName, `/\`) {
		return "", errors.New("YOUTUBE_COLLECTOR_LOG_FILE_NAME must be a file name without path separators")
	}

	return fileName, nil
}
