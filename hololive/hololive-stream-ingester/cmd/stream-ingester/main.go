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
	"log/slog"
	"os"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/automaxprocs"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
	sharedtelemetry "github.com/kapu/hololive-shared/pkg/telemetry"

	"github.com/kapu/hololive-stream-ingester/internal/app"
)

// Version: 빌드 시 ldflags로 주입됨 (예: -ldflags="-X main.Version=1.0.0")
var Version = "dev"

func main() {
	automaxprocs.Init(nil)
	health.Init(Version)

	var exitCode int
	defer func() {
		os.Exit(exitCode)
	}()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load stream ingester config: %v\n", err)
		exitCode = 1
		return
	}

	logger, err := sharedlogging.EnableFileLoggingWithLevel(sharedlogging.Config{
		Dir:        cfg.Logging.Dir,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}, "stream-ingester.log", cfg.Logging.Level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		exitCode = 1
		return
	}

	otelProvider, err := sharedtelemetry.NewProvider(context.Background(), sharedtelemetry.Config{
		Enabled:               cfg.Telemetry.Enabled,
		MetricsEnabled:        cfg.Telemetry.MetricsEnabled,
		MetricsExportInterval: cfg.Telemetry.MetricsExportInterval,
		ServiceName:           cfg.Telemetry.ServiceName,
		ServiceVersion:        cfg.Telemetry.ServiceVersion,
		Environment:           cfg.Telemetry.Environment,
		OTLPEndpoint:          cfg.Telemetry.OTLPEndpoint,
		OTLPInsecure:          cfg.Telemetry.OTLPInsecure,
		SampleRate:            cfg.Telemetry.SampleRate,
	})
	if err != nil {
		logger.Error("otel_init_failed", slog.Any("err", err))
		exitCode = 1
		return
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := otelProvider.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("otel_shutdown_failed", slog.Any("err", shutdownErr))
		}
	}()

	if otelProvider.IsEnabled() {
		logger.Info("otel_enabled",
			slog.String("service", cfg.Telemetry.ServiceName),
			slog.String("endpoint", cfg.Telemetry.OTLPEndpoint),
			slog.Bool("tracing_enabled", otelProvider.IsTracingEnabled()),
			slog.Bool("metrics_enabled", otelProvider.IsMetricsEnabled()),
			slog.Duration("metrics_export_interval", cfg.Telemetry.MetricsExportInterval),
			slog.Float64("sample_rate", cfg.Telemetry.SampleRate),
		)
	}

	logger.Info("Stream Ingester starting...",
		slog.String("version", Version),
		slog.String("log_level", cfg.Logging.Level),
		slog.Int("port", cfg.Server.Port),
	)

	buildCtx, buildCancel := context.WithTimeout(context.Background(), 1*time.Minute)
	runtime, err := app.BuildStreamIngesterRuntime(buildCtx, cfg, logger)
	buildCancel()
	if err != nil {
		logger.Error("Failed to build stream ingester runtime", slog.Any("error", err))
		exitCode = 1
		return
	}
	defer runtime.Close()

	runtime.Run()
}
