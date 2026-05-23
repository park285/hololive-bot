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
	"github.com/kapu/hololive-shared/pkg/health"
	sharedlogging "github.com/park285/hololive-bot/shared-go/pkg/logging"
	"github.com/park285/hololive-bot/shared-go/pkg/runtime/automaxprocs"
	"github.com/park285/hololive-bot/shared-go/pkg/runtime/bootstrap"
)

var Version = "dev"

func main() {
	os.Exit(bootstrap.Run(bootstrap.Options[*config.LLMSchedulerConfig, *app.LLMSchedulerRuntime]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		LoadConfig:             config.LoadLLMScheduler,
		LoadConfigErrorMessage: "Failed to load llm scheduler config",
		LoggerConfig: func(schedulerConfig *config.LLMSchedulerConfig) sharedlogging.Config {
			return sharedlogging.Config{
				Dir:        schedulerConfig.Logging.Dir,
				MaxSizeMB:  schedulerConfig.Logging.MaxSizeMB,
				MaxBackups: schedulerConfig.Logging.MaxBackups,
				MaxAgeDays: schedulerConfig.Logging.MaxAgeDays,
				Compress:   schedulerConfig.Logging.Compress,
			}
		},
		LoggerFileName: "llm-scheduler.log",
		LoggerLevel: func(schedulerConfig *config.LLMSchedulerConfig) string {
			return schedulerConfig.Logging.Level
		},
		StartupMessage: "LLM Scheduler starting...",
		StartupFields: func(schedulerConfig *config.LLMSchedulerConfig) []any {
			return []any{slog.Int("port", schedulerConfig.Server.Port)}
		},
		BuildTimeout:      time.Minute,
		BuildRuntime:      app.BuildLLMSchedulerRuntime,
		BuildErrorMessage: "Failed to build llm scheduler runtime",
	}))
}
