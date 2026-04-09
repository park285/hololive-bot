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
	"fmt"
	"log/slog"
	"time"

	appproviders "github.com/kapu/hololive-kakao-bot-go/internal/app/providers"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
)

func initAlarmDependencies(
	chzzkCfg config.ChzzkConfig,
	twitchCfg config.TwitchConfig,
	advanceMinutes []int,
	scraperProxyEnabled bool,
	cacheService cache.Client,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*alarmDependencies, error) {
	httpClient := httputil.NewExternalAPIClient(10 * time.Second)
	chzzkClient := ProvideChzzkClient(httpClient, chzzkCfg, logger)
	twitchClient := ProvideTwitchClient(twitchCfg, logger)
	memberDataProvider := memberServiceAdapter

	resolved := appproviders.ResolveAlarmAdvanceMinutes(advanceMinutes, scraperProxyEnabled, logger)

	alarmService, err := ProvideAlarmService(resolved, cacheService, holodexService, chzzkClient, twitchClient, memberDataProvider, alarmRepository, logger)
	if err != nil {
		return nil, fmt.Errorf("provide alarm service: %w", err)
	}

	return &alarmDependencies{
		alarmService:       alarmService,
		memberDataProvider: memberDataProvider,
		chzzkClient:        chzzkClient,
		twitchClient:       twitchClient,
	}, nil
}

func initAlarmModeComponents(
	ctx context.Context,
	cfg *config.Config,
	infra *infraResources,
	holodexService *holodex.Service,
	memberServiceAdapter member.DataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*alarmModeComponents, error) {
	alarmDeps, alarmErr := initAlarmDependencies(
		cfg.Chzzk,
		cfg.Twitch,
		cfg.Notification.AdvanceMinutes,
		cfg.Scraper.ProxyEnabled,
		infra.cacheService,
		holodexService,
		memberServiceAdapter,
		alarmRepository,
		logger,
	)
	if alarmErr != nil {
		return nil, alarmErr
	}

	if warnErr := alarmDeps.alarmService.WarmCacheFromDB(ctx); warnErr != nil {
		logger.Warn("Failed to warm alarm cache from DB", "error", warnErr)
	}

	return &alarmModeComponents{
		alarmCRUD:        alarmDeps.alarmService,
		alarmService:     alarmDeps.alarmService,
		chzzkClient:      alarmDeps.chzzkClient,
		twitchClient:     alarmDeps.twitchClient,
		memberDataSource: alarmDeps.memberDataProvider,
	}, nil
}
