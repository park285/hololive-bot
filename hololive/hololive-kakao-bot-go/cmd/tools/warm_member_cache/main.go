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
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"

	"github.com/kapu/hololive-kakao-bot-go/internal/app"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load_config_failed", slog.Any("error", err))
		return
	}

	logger, err := sharedlogging.EnableFileLoggingWithLevel(sharedlogging.Config{
		Dir:        cfg.Logging.Dir,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}, "warm_member_cache.log", cfg.Logging.Level)
	if err != nil {
		slog.Error("init_logger_failed", slog.Any("error", err))
		return
	}

	logger.Info("Manual member list cache refresh started")

	_, cleanup, err := app.InitializeWarmMemberCache(ctx, cfg, logger)
	if err != nil {
		logger.Error("Manual cache refresh failed", slog.Any("error", err))
		return
	}
	defer cleanup()

	logger.Info("Manual member list cache refresh completed successfully")
}
