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
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
)

var Version = "dev"

func main() {
	if handled, exitCode := entrypoint.RunWorkerProfileCheck(os.Args[1:], os.Stderr, "hololive-api", func() error {
		if _, err := loadRuntime(); err != nil {
			return fmt.Errorf("load api worker profile: %w", err)
		}

		return nil
	}); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := runConfigCheck(os.Args[1:], os.Stderr, func() error {
		if _, err := loadRuntime(); err != nil {
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
		LoadConfig:             loadRuntime,
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

func loadRuntime() (*apiconfig.RuntimeConfig, error) {
	config, err := apiconfig.LoadRuntime()
	if err != nil {
		return nil, fmt.Errorf("load API runtime config: %w", err)
	}

	if err := databaseproviders.Preflight(&config.Bot.Postgres, databaseproviders.RuntimePool{Name: "bot", ReservedConns: 1}); err != nil {
		return nil, fmt.Errorf("preflight bot database: %w", err)
	}

	if err := databaseproviders.Preflight(&config.Admin.Postgres, databaseproviders.RuntimePool{Name: "admin"}); err != nil {
		return nil, fmt.Errorf("preflight admin database: %w", err)
	}

	youtubePostgres := config.Bot.Postgres

	youtubePostgres.PoolMinConns = config.YouTube.PostgresPoolMinConns
	youtubePostgres.PoolMaxConns = config.YouTube.PostgresPoolMaxConns

	if err := databaseproviders.Preflight(&youtubePostgres, databaseproviders.RuntimePool{Name: "youtube_plane", ReservedConns: 1}); err != nil {
		return nil, fmt.Errorf("preflight YouTube database: %w", err)
	}

	return config, nil
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
