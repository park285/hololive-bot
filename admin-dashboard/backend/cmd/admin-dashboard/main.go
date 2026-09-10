package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"

	adminbootstrap "github.com/kapu/admin-dashboard/internal/bootstrap"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/testaccountcli"
)

var Version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "test-account" {
		if err := testaccountcli.Run(context.Background(), os.Args[2:], os.Stdout); err != nil {
			// 설정·저장소 오류가 자격증명 값을 포함할 수 있으므로 원문을 터미널로 내보내지 않습니다.
			fmt.Fprintln(os.Stderr, "test-account failed; verify arguments, private output directory and current account status before retrying")
			os.Exit(1)
		}

		return
	}

	os.Exit(bootstrap.Options[*config.Config, *adminbootstrap.Runtime]{
		Version: Version,
		Initialize: func(string) {
			automaxprocs.Init(nil)
			gin.SetMode(gin.ReleaseMode)
		},
		LoadConfig:             config.Load,
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
		BuildRuntime:      adminbootstrap.New,
		BuildErrorMessage: "Failed to assemble admin dashboard runtime",
	}.Run())
}
