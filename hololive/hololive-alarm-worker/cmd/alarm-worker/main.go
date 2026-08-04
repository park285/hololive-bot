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
	"log/slog"
	"os"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-alarm-worker/internal/app/workerapp"
	"github.com/kapu/hololive-alarm-worker/internal/service/workerruntime"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/observability"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/pkg/telemetry"
)

var Version = "dev"

func main() {
	os.Exit(bootstrap.Run(bootstrap.Options[*settings.Config, *observability.ManagedRuntime[*workerruntime.AlarmWorkerRuntime]]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		LoadConfig:             settings.LoadAlarmWorkerRuntime,
		LoadConfigErrorMessage: "Failed to load config",
		LoggerConfig: func(appConfig *settings.Config) sharedlogging.Config {
			return sharedlogging.Config{
				Dir:        appConfig.Logging.Dir,
				MaxSizeMB:  appConfig.Logging.MaxSizeMB,
				MaxBackups: appConfig.Logging.MaxBackups,
				MaxAgeDays: appConfig.Logging.MaxAgeDays,
				Compress:   appConfig.Logging.Compress,
			}
		},
		LoggerFileName: "alarm-worker.log",
		LoggerLevel: func(appConfig *settings.Config) string {
			return appConfig.Logging.Level
		},
		StartupMessage: "Hololive Alarm Worker starting...",
		BuildTimeout:   constants.AppTimeout.Build,
		BuildRuntime: func(
			ctx context.Context,
			appConfig *settings.Config,
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
	}))
}

func alarmWorkerTelemetryConfig(appConfig *settings.Config, version string) telemetry.Config {
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
