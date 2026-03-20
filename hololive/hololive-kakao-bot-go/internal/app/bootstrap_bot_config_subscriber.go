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
	"context"
	"log/slog"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

// buildBotConfigSubscriber: Bot용 ConfigSubscriber를 생성합니다.
// Scraper_proxy / alarm_advance_minutes 두 가지 설정 변경을 수신하여 적용합니다.
func buildBotConfigSubscriber(
	ctx context.Context,
	deps botConfigSubscriberDependencies,
	runtimeDeps botConfigSubscriberRuntimeDependencies,
	scraperScheduler *poller.Scheduler,
	logger *slog.Logger,
) *configsub.Subscriber {
	applyFn := configsub.NewApplyFn(logger, configsub.ApplyHandlers{
		ScraperProxy: func(payload contractssettings.ScraperProxyPayloadV1) {
			applyScraperProxyToggle(payload.Enabled, runtimeDeps.youtubeService, runtimeDeps.holodexService, scraperScheduler, logger)
			// 설정 파일에도 반영
			current := deps.settings.Get()

			current.ScraperProxyEnabled = payload.Enabled
			if err := deps.settings.Update(current); err != nil {
				logger.Warn("Failed to persist scraper_proxy setting", slog.Any("error", err))
			}
		},
		AlarmAdvanceMinutes: func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
			targets := runtimeDeps.alarmCRUD.UpdateAlarmAdvanceMinutes(ctx, payload.Minutes)
			logger.Info("Applied alarm advance minutes via pub/sub",
				slog.Int("minutes", payload.Minutes),
				slog.Any("targets", targets),
			)
			// 설정 파일에도 반영
			current := deps.settings.Get()

			current.AlarmAdvanceMinutes = payload.Minutes
			if err := deps.settings.Update(current); err != nil {
				logger.Warn("Failed to persist alarm_advance_minutes setting", slog.Any("error", err))
			}
		},
	})

	return configsub.New(deps.cache.GetClient(), applyFn, logger)
}
