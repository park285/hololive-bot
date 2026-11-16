package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	_ "time/tzdata"

	"github.com/park285/shared-go/v2/pkg/health"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"

	runtimeapp "github.com/kapu/hololive-api/internal/app"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/entrypoint"
)

var Version = "dev"

func main() {
	if handled, exitCode := entrypoint.RunWorkerProfileCheck(os.Args[1:], os.Stderr, "hololive-api", func() error {
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

	os.Exit(bootstrap.Options[*apiconfig.RuntimeConfig, *runtimeapp.Runtime]{
		Version: Version,
		Initialize: func(version string) {
			health.Init(version)
		},
		LoadConfig:             apiconfig.LoadRuntime,
		LoadConfigErrorMessage: "Failed to load hololive-api config",
		NewLogger: func(config *apiconfig.RuntimeConfig) (*slog.Logger, io.Closer, error) {
			return entrypoint.NewFileLogger(config.Logging, "hololive-api.log")
		},
		LoggerLevel: func(config *apiconfig.RuntimeConfig) string {
			return config.Logging.Level
		},
		StartupMessage: "Hololive unified API starting...",
		StartupFields: func(config *apiconfig.RuntimeConfig) []any {
			return []any{
				slog.Int("bot_port", config.Bot.Server.Port),
				slog.Int("admin_port", config.Admin.Server.Port),
			}
		},
		BuildTimeout:      constants.AppTimeout.Build,
		BuildRuntime:      runtimeapp.BuildRuntime,
		BuildErrorMessage: "Failed to assemble hololive-api runtime",
	}.Run())
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
