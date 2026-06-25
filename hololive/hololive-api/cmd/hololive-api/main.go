package main

import (
	"log/slog"
	"os"

	"github.com/kapu/hololive-api/internal/app"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/pkg/runtime/bootstrap"
)

var Version = "dev"

func main() {
	os.Exit(bootstrap.Run(bootstrap.Options[*config.HololiveAPIConfig, *app.Runtime]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		LoadConfig:             config.LoadHololiveAPIRuntime,
		LoadConfigErrorMessage: "Failed to load hololive-api config",
		LoggerConfig: func(appConfig *config.HololiveAPIConfig) sharedlogging.Config {
			return sharedlogging.Config{
				Dir:        appConfig.Logging.Dir,
				MaxSizeMB:  appConfig.Logging.MaxSizeMB,
				MaxBackups: appConfig.Logging.MaxBackups,
				MaxAgeDays: appConfig.Logging.MaxAgeDays,
				Compress:   appConfig.Logging.Compress,
			}
		},
		LoggerFileName: "hololive-api.log",
		LoggerLevel: func(appConfig *config.HololiveAPIConfig) string {
			return appConfig.Logging.Level
		},
		StartupMessage: "Hololive unified API starting...",
		StartupFields: func(appConfig *config.HololiveAPIConfig) []any {
			return []any{
				slog.Int("bot_port", appConfig.Bot.Server.Port),
				slog.Int("admin_port", appConfig.Admin.Server.Port),
				slog.Int("llm_port", appConfig.LLM.Server.Port),
			}
		},
		BuildTimeout:      constants.AppTimeout.Build,
		BuildRuntime:      app.BuildRuntime,
		BuildErrorMessage: "Failed to assemble hololive-api runtime",
	}))
}
