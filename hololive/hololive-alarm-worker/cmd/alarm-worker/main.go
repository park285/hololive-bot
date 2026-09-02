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
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/park285/shared-go/v2/pkg/health"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/telemetry"

	"github.com/kapu/hololive-alarm-worker/internal/app/workerapp"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-shared/pkg/config/settings/alarmworker"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/observability"
)

var Version = "dev"

func main() {
	if handled, exitCode := runWorkerProfileCheck(os.Args[1:], os.Stderr, func() error {
		if _, err := alarmworker.LoadWorkerProfile(); err != nil {
			return fmt.Errorf("load alarm worker profile: %w", err)
		}

		return nil
	}); handled {
		os.Exit(exitCode)
	}

	os.Exit(bootstrap.Options[*alarmworker.RuntimeConfig, *observability.ManagedRuntime[*workerruntime.AlarmWorkerRuntime]]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		LoadConfig:             alarmworker.LoadRuntime,
		LoadConfigErrorMessage: "Failed to load config",
		LoggerConfig: func(appConfig *alarmworker.RuntimeConfig) sharedlogging.Config {
			return sharedlogging.Config{
				Dir:        appConfig.Logging.Dir,
				MaxSizeMB:  appConfig.Logging.MaxSizeMB,
				MaxBackups: appConfig.Logging.MaxBackups,
				MaxAgeDays: appConfig.Logging.MaxAgeDays,
				Compress:   appConfig.Logging.Compress,
			}
		},
		LoggerFileName: "alarm-worker.log",
		LoggerLevel: func(appConfig *alarmworker.RuntimeConfig) string {
			return appConfig.Logging.Level
		},
		StartupMessage: "Hololive Alarm Worker starting...",
		BuildTimeout:   constants.AppTimeout.Build,
		BuildRuntime: func(
			ctx context.Context,
			appConfig *alarmworker.RuntimeConfig,
			logger *slog.Logger,
		) (*observability.ManagedRuntime[*workerruntime.AlarmWorkerRuntime], error) {
			traceConfig := alarmWorkerTelemetryConfig(appConfig, Version)

			return observability.BuildRuntime(
				ctx,
				&traceConfig,
				logger,
				func(ctx context.Context) (*workerruntime.AlarmWorkerRuntime, error) {
					return workerapp.BuildAlarmWorkerRuntime(ctx, appConfig, logger)
				},
			)
		},
		BuildErrorMessage: "Failed to assemble alarm worker runtime",
	}.Run())
}

func runWorkerProfileCheck(args []string, stderr io.Writer, load func() error) (handled bool, exitCode int) {
	if len(args) != 1 || args[0] != "--check-worker-profile" {
		return false, 0
	}

	if err := load(); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Failed to load alarm-worker worker profile: %v\n", err); writeErr != nil {
			return true, 1
		}

		return true, 1
	}

	if _, err := fmt.Fprintln(stderr, "alarm-worker worker profile valid"); err != nil {
		return true, 1
	}

	return true, 0
}

func alarmWorkerTelemetryConfig(appConfig *alarmworker.RuntimeConfig, version string) telemetry.Config {
	return telemetry.Config{
		Enabled:        appConfig.Tracing.Enabled,
		ServiceName:    "hololive-alarm-worker",
		ServiceVersion: version,
		Environment:    appConfig.Environment,
		OTLPEndpoint:   appConfig.Tracing.Endpoint,
		OTLPInsecure:   appConfig.Tracing.Insecure,
		SampleRate:     appConfig.Tracing.SampleRate,
	}
}
