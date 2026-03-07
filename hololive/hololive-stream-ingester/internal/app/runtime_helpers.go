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

package app

import (
	"fmt"
	"log/slog"
	"reflect"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

type scraperProxyRuntimeService interface {
	SetScraperProxyEnabled(enabled bool) bool
	ScraperProxyEnabled() bool
}

func normalizeScraperProxyRuntimeService(service scraperProxyRuntimeService) scraperProxyRuntimeService {
	if service == nil {
		return nil
	}

	value := reflect.ValueOf(service)
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func:
		if value.IsNil() {
			return nil
		}
	}

	return service
}

// ProvideAPIAddr: 관리자 서버가 리슨할 주소를 반환합니다.
func ProvideAPIAddr(cfg *config.Config) string {
	return fmt.Sprintf(":%d", cfg.Server.Port)
}

// ProvideYouTubeService: YouTube 서비스 인스턴스를 제공합니다.
func ProvideYouTubeService(ytStack *providers.YouTubeStack) youtube.Service {
	if ytStack == nil {
		return nil
	}
	return ytStack.Service
}

func applyScraperProxyToggle(
	enabled bool,
	youtubeService youtube.Service,
	holodexService scraperProxyRuntimeService,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) {
	holodexService = normalizeScraperProxyRuntimeService(holodexService)

	youtubeApplied := false
	holodexApplied := false
	schedulerApplied := 0

	if youtubeService != nil {
		youtubeApplied = youtubeService.SetScraperProxyEnabled(enabled)
	}
	if holodexService != nil {
		holodexApplied = holodexService.SetScraperProxyEnabled(enabled)
	}
	if scraperScheduler != nil {
		schedulerApplied = scraperScheduler.SetProxyEnabled(enabled)
	}

	logger.Info("Applied scraper proxy toggle",
		slog.Bool("enabled", enabled),
		slog.Bool("youtube_applied", youtubeApplied),
		slog.Bool("holodex_applied", holodexApplied),
		slog.Int("scheduler_pollers_applied", schedulerApplied),
	)
}
