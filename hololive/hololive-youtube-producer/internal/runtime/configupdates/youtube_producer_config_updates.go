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

package configupdates

import (
	"log/slog"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

// buildYouTubeProducerConfigSubscriber: youtube-producer 전용 ConfigSubscriber를 구성한다.
// alarm_advance_minutes 설정은 youtube-producer에서 불필요하므로 scraper_proxy만 처리한다.
func buildYouTubeProducerConfigSubscriber(
	cacheService cache.Client,
	settingsService settings.ReadWriter,
	holodexService *holodex.Service,
	ytStack *providers.YouTubeStack,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		ScraperProxy: func(payload contractssettings.ScraperProxyPayloadV1) {
			sharedsettings.ApplyScraperProxyToggle(payload.Enabled, ytStack.GetService(), holodexService, scraperScheduler, logger)
			current := settingsService.Get()
			current.ScraperProxyEnabled = payload.Enabled
			if err := settingsService.Update(current); err != nil {
				logger.Warn("Failed to persist scraper_proxy setting", slog.Any("error", err))
			}
		},
		AlarmAdvanceMinutes: func(contractssettings.AlarmAdvanceMinutesPayloadV1) {
			// youtube-producer는 alarm dispatch를 담당하지 않으므로 무시
			logger.Debug("Ignoring alarm_advance_minutes config update (youtube-producer)")
		},
	})

	return configsub.New(cacheService.GetClient(), applyFn, logger)
}
