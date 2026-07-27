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

package botruntime

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"

	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	"github.com/kapu/hololive-shared/pkg/domain"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

func initAlarmDependencies(
	chzzkConfig settings.ChzzkConfig,
	twitchConfig *settings.TwitchConfig,
	advanceMinutes []int,
	scraperProxyEnabled bool,
	cacheService cache.Client,
	holodexService *holodexprovider.Service,
	memberServiceAdapter domain.MemberDataProvider, alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*appbootstrap.AlarmDependencies, error) {
	deps, err := appbootstrap.InitAlarmDependencies(chzzkConfig, twitchConfig, advanceMinutes, scraperProxyEnabled, cacheService, holodexService, memberServiceAdapter, alarmRepository, logger)
	if err != nil {
		return nil, err
	}
	return deps, nil
}

func initAlarmModeComponents(
	ctx context.Context,
	appConfig *settings.Config,
	infra *sharedmodules.InfraModule,
	holodexService *holodexprovider.Service,
	memberServiceAdapter domain.MemberDataProvider, alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*appbootstrap.AlarmModeComponents, error) {
	components, err := appbootstrap.InitAlarmModeComponents(ctx, appConfig, infra, holodexService, memberServiceAdapter, alarmRepository, logger)
	if err != nil {
		return nil, err
	}
	return components, nil
}
