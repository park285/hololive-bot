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
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/producerruntime"
	"github.com/park285/shared-go/pkg/envutil"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/automaxprocs"
	"github.com/park285/shared-go/pkg/runtime/bootstrap"
)

var Version = "dev"

func main() {
	os.Exit(bootstrap.Run(bootstrap.Options[*settings.Config, *producerruntime.YouTubeProducerRuntime]{
		Version: Version,
		Initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		LoadConfig:             settings.LoadYouTubeProducerRuntime,
		LoadConfigErrorMessage: "Failed to load youtube producer config",
		LoggerConfig: func(appConfig *settings.Config) sharedlogging.Config {
			return sharedlogging.Config{
				Dir:        appConfig.Logging.Dir,
				MaxSizeMB:  appConfig.Logging.MaxSizeMB,
				MaxBackups: appConfig.Logging.MaxBackups,
				MaxAgeDays: appConfig.Logging.MaxAgeDays,
				Compress:   appConfig.Logging.Compress,
			}
		},
		LoggerFileName: youtubeProducerLogFileName(),
		LoggerLevel: func(appConfig *settings.Config) string {
			return appConfig.Logging.Level
		},
		StartupMessage: "YouTube Producer starting...",
		StartupFields: func(appConfig *settings.Config) []any {
			return []any{slog.Int("port", appConfig.Server.Port)}
		},
		BuildTimeout:      time.Minute,
		BuildRuntime:      producerruntime.BuildYouTubeProducerRuntime,
		BuildErrorMessage: "Failed to build youtube producer runtime",
	}))
}

func youtubeProducerLogFileName() string {
	fileName := envutil.String("YOUTUBE_PRODUCER_LOG_FILE_NAME", "")
	if fileName == "" || strings.Contains(fileName, "/") || strings.Contains(fileName, "\\") {
		return "youtube-producer.log"
	}
	return fileName
}
