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

	"github.com/park285/shared-go/core/envutil"
	"github.com/park285/shared-go/v2/pkg/health"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"

	collectorconfig "github.com/kapu/hololive-collector/internal/config"
	"github.com/kapu/hololive-collector/internal/runtime/collectorruntime"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/entrypoint"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
)

var Version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--check-database-endpoint" {
		if err := checkDatabaseEndpoint(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "collector database endpoint check failed:", err)
			os.Exit(1)
		}

		fmt.Println("collector database endpoint verified")

		return
	}

	if handled, exitCode := entrypoint.RunWorkerProfileCheck(os.Args[1:], os.Stderr, "collector", func() error {
		if _, err := loadRuntime(); err != nil {
			return fmt.Errorf("load collector worker profile: %w", err)
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
		LoadConfig:             loadRuntime,
		LoadConfigErrorMessage: "Failed to load collector config",
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
		StartupMessage: "Collector starting...",
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
		BuildErrorMessage: "Failed to build collector runtime",
	}.Run())
}

func loadRuntime() (*collectorconfig.RuntimeConfig, error) {
	config, err := collectorconfig.LoadRuntime()
	if err != nil {
		return nil, fmt.Errorf("load collector runtime config: %w", err)
	}

	if err := databaseproviders.Preflight(&config.Postgres, databaseproviders.RuntimePool{Name: "collector", ReservedConns: 1}); err != nil {
		return nil, fmt.Errorf("preflight collector database: %w", err)
	}

	return config, nil
}

type startupOptions struct {
	timeout         time.Duration
	waitForDatabase bool
}

func parseStartupOptions(args []string) (startupOptions, bool) {
	options := startupOptions{timeout: constants.AppTimeout.Build}
	flags := flag.NewFlagSet("collector", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.DurationVar(&options.timeout, "startup-timeout", options.timeout, "runtime build timeout (positive, at most 90s)")
	flags.BoolVar(&options.waitForDatabase, "wait-for-database", false, "wait for initial database connection deadlines within the startup budget")

	if flags.Parse(args) != nil || flags.NArg() != 0 || options.timeout <= 0 || options.timeout > 90*time.Second {
		return startupOptions{}, false
	}

	return options, true
}

func youtubeCollectorLogFileName() (string, error) {
	fileName := envutil.String("COLLECTOR_LOG_FILE_NAME", "")
	if fileName == "" {
		return "collector.log", nil
	}

	if strings.ContainsAny(fileName, `/\`) {
		return "", errors.New("COLLECTOR_LOG_FILE_NAME must be a file name without path separators")
	}

	return fileName, nil
}
