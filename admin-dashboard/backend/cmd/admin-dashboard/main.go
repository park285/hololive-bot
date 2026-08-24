package main

import (
	"os"
	"time"

	"github.com/gin-gonic/gin"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"

	"github.com/kapu/admin-dashboard/internal/app"
	"github.com/kapu/admin-dashboard/internal/config"
)

var Version = "dev"

func main() {
	os.Exit(bootstrap.Options[*config.Config, *app.Runtime]{
		Version: Version,
		Initialize: func(string) {
			automaxprocs.Init(nil)
			gin.SetMode(gin.ReleaseMode)
		},
		LoadConfig:             config.LoadSecure,
		LoadConfigErrorMessage: "Failed to load admin dashboard config",
		LoggerConfig: func(cfg *config.Config) sharedlogging.Config {
			return sharedlogging.Config{
				Level:      cfg.Logging.Level,
				Dir:        cfg.Logging.Dir,
				MaxSizeMB:  cfg.Logging.MaxSizeMB,
				MaxBackups: cfg.Logging.MaxBackups,
				MaxAgeDays: cfg.Logging.MaxAgeDays,
				Compress:   cfg.Logging.Compress,
			}
		},
		LoggerFileName: "admin-dashboard.log",
		LoggerLevel: func(cfg *config.Config) string {
			return cfg.Logging.Level
		},
		StartupMessage:    "Admin dashboard starting...",
		BuildTimeout:      30 * time.Second,
		BuildRuntime:      app.New,
		BuildErrorMessage: "Failed to assemble admin dashboard runtime",
	}.Run())
}
