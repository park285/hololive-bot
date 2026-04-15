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
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/app"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/bootstrap"
)

var Version = "dev"

func main() {
	os.Exit(bootstrap.Run(bootstrap.Options[*config.LLMSchedulerConfig, *app.LLMSchedulerRuntime]{
		Version:                Version,
		LoadConfig:             config.LoadLLMScheduler,
		LoadConfigErrorMessage: "Failed to load llm scheduler config",
		LoggerConfig: func(cfg *config.LLMSchedulerConfig) sharedlogging.Config {
			return sharedlogging.Config{
				Dir:        cfg.Logging.Dir,
				MaxSizeMB:  cfg.Logging.MaxSizeMB,
				MaxBackups: cfg.Logging.MaxBackups,
				MaxAgeDays: cfg.Logging.MaxAgeDays,
				Compress:   cfg.Logging.Compress,
			}
		},
		LoggerFileName: "llm-scheduler.log",
		LoggerLevel: func(cfg *config.LLMSchedulerConfig) string {
			return cfg.Logging.Level
		},
		StartupMessage: "LLM Scheduler starting...",
		StartupFields: func(cfg *config.LLMSchedulerConfig) []any {
			return []any{slog.Int("port", cfg.Server.Port)}
		},
		BuildTimeout:      time.Minute,
		BuildRuntime:      app.BuildLLMSchedulerRuntime,
		BuildErrorMessage: "Failed to build llm scheduler runtime",
	}))
}
