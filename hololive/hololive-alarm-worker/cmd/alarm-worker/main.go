// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
