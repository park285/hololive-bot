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
	"io"
	"log/slog"
	"os"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/pkg/runtime/bootstrap"

	"github.com/kapu/hololive-kakao-bot-go/internal/app"
)

var Version = "dev"

func main() {
	var logCloser io.Closer
	code := bootstrap.Run(bootstrap.Options[*config.Config, *app.BotRuntime]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		LoadConfig:             config.Load,
		LoadConfigErrorMessage: "Failed to load config",
		NewLogger: func(appConfig *config.Config) (*slog.Logger, error) {
			logger, closer, err := sharedlogging.EnableFileLoggingWithOptions(sharedlogging.Config{
				Level:      appConfig.Logging.Level,
				Dir:        appConfig.Logging.Dir,
				MaxSizeMB:  appConfig.Logging.MaxSizeMB,
				MaxBackups: appConfig.Logging.MaxBackups,
				MaxAgeDays: appConfig.Logging.MaxAgeDays,
				Compress:   appConfig.Logging.Compress,
			}, "bot.log", sharedlogging.Options{AsyncStdout: true})
			logCloser = closer
			return logger, err
		},
		LoggerLevel: func(appConfig *config.Config) string {
			return appConfig.Logging.Level
		},
		StartupMessage:    "Hololive KakaoTalk Bot starting...",
		BuildTimeout:      constants.AppTimeout.Build,
		BuildRuntime:      app.BuildRuntime,
		BuildErrorMessage: "Failed to assemble application services",
	})
	if logCloser != nil {
		if err := logCloser.Close(); err != nil {
			slog.Error("log closer close failed", slog.Any("error", err))
		}
	}
	os.Exit(code)
}
