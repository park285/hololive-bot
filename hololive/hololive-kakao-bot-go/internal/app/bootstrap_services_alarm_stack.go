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

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/settings"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
)

type alarmYouTubeStackComponents struct {
	alarmMode       *alarmModeComponents
	memberMatcher   *matcher.MemberMatcher
	youTubeStack    *providers.YouTubeStack
	activityLogger  *activity.Logger
	settingsService settings.ReadWriter
}

func initAlarmYouTubeStack(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	foundation *scraperHolodexProfileFoundation,
	irisClient iris.Client,
	formatter *adapter.ResponseFormatter,
	logger *slog.Logger,
) (*alarmYouTubeStackComponents, error) {
	alarmRepository := ProvideAlarmRepository(infra.postgresService, logger)

	alarmMode, err := initAlarmModeComponents(
		ctx,
		cfg,
		infra,
		foundation.holodexService,
		foundation.memberServiceAdapter,
		alarmRepository,
		logger,
	)
	if err != nil {
		return nil, err
	}

	memberMatcher := ProvideMemberMatcher(
		ctx,
		alarmMode.memberDataSource,
		infra.cacheService,
		foundation.holodexService,
		logger,
	)
	youTubeStatsRepository := providers.ProvideYouTubeStatsRepository(infra.postgresService, logger)
	youTubeStack := providers.ProvideYouTubeStack(
		ctx,
		cfg.YouTube,
		cfg.Scraper,
		infra.cacheService,
		foundation.holodexService,
		foundation.memberServiceAdapter,
		youTubeStatsRepository,
		alarmMode.alarmService,
		irisClient,
		formatter,
		foundation.sharedRL,
		logger,
	)

	return &alarmYouTubeStackComponents{
		alarmMode:      alarmMode,
		memberMatcher:  memberMatcher,
		youTubeStack:   youTubeStack,
		activityLogger: ProvideActivityLogger(logger),
		settingsService: providers.ProvideSettingsService(
			cfg.Notification.AdvanceMinutes,
			cfg.Scraper.ProxyEnabled,
			logger,
		),
	}, nil
}
