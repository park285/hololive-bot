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

	"github.com/kapu/hololive-dispatcher-go/internal/app"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
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

	cfg, err := app.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load dispatcher config: %v\n", err)
		exitCode = 1
		return
	}

	logger, err := sharedlogging.EnableFileLoggingWithLevel(sharedlogging.Config{
		Dir:        cfg.Logging.Dir,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}, "dispatcher-go.log", cfg.Logging.Level)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		exitCode = 1
		return
	}

	logger.Info("Dispatcher Go starting...",
		slog.String("version", Version),
		slog.String("log_level", cfg.Logging.Level),
		slog.Int("port", cfg.Server.Port),
		slog.String("queue_key", cfg.Dispatch.QueueKey),
	)

	buildCtx, buildCancel := context.WithTimeout(context.Background(), 1*time.Minute)
	runtime, err := app.BuildRuntime(buildCtx, cfg, logger)
	buildCancel()
	if err != nil {
		logger.Error("Failed to build dispatcher runtime", slog.Any("error", err))
		exitCode = 1
		return
	}
	defer runtime.Close()

	runtime.Run()
}
