package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/park285/shared-go/v2/pkg/health"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"

	"github.com/kapu/hololive-alarm-worker/internal/app/workerapp"
	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/entrypoint"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
)

var Version = "dev"

func main() {
	if handled, exitCode := entrypoint.RunWorkerProfileCheck(os.Args[1:], os.Stderr, "alarm-worker", func() error {
		if _, err := loadRuntime(); err != nil {
			return fmt.Errorf("load alarm worker profile: %w", err)
		}

		return nil
	}); handled {
		os.Exit(exitCode)
	}

	os.Exit(bootstrap.Options[*workerconfig.RuntimeConfig, *workerruntime.AlarmWorkerRuntime]{
		Version: Version,
		Initialize: func(version string) {
			health.Init(version)
		},
		LoadConfig:             loadRuntime,
		LoadConfigErrorMessage: "Failed to load config",
		NewLogger: func(appConfig *workerconfig.RuntimeConfig) (*slog.Logger, io.Closer, error) {
			return entrypoint.NewFileLogger(appConfig.Logging, "alarm-worker.log")
		},
		LoggerLevel: func(appConfig *workerconfig.RuntimeConfig) string {
			return appConfig.Logging.Level
		},
		StartupMessage:    "Hololive Alarm Worker starting...",
		BuildTimeout:      constants.AppTimeout.Build,
		BuildRuntime:      workerapp.BuildAlarmWorkerRuntime,
		BuildErrorMessage: "Failed to assemble alarm worker runtime",
	}.Run())
}

func loadRuntime() (*workerconfig.RuntimeConfig, error) {
	config, err := workerconfig.LoadRuntime()
	if err != nil {
		return nil, fmt.Errorf("load alarm worker runtime config: %w", err)
	}

	if err := databaseproviders.Preflight(&config.Postgres, databaseproviders.RuntimePool{Name: "alarm_worker", ReservedConns: 1}); err != nil {
		return nil, fmt.Errorf("preflight alarm worker database: %w", err)
	}

	return config, nil
}
