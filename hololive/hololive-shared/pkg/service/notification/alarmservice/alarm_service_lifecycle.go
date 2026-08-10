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

package alarmservice

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/cache"

	"github.com/kapu/hololive-shared/pkg/service/notification/alarmcache"
	"github.com/kapu/hololive-shared/pkg/service/notification/platformmap"

	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

const (
	alarmServiceCloseTimeout = 3 * time.Second
	alarmPersistTaskTimeout  = alarmServiceCloseTimeout
)

var (
	_ domain.AlarmCRUD          = (*AlarmService)(nil)
	_ domain.AlarmDispatchState = (*AlarmService)(nil)
)

func NewAlarmService(
	cacheClient cache.Client,
	holodexService *holodexprovider.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	memberData domain.MemberDataProvider,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
	advanceMinutes []int,
) (*AlarmService, error) {
	if cacheClient == nil {
		return nil, fmt.Errorf("new alarm service: cache client is nil")
	}

	if logger == nil {
		logger = slog.Default()
	}

	initAlarmMetrics()

	targetPolicy := sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(advanceMinutes))

	var writer alarmWriter
	if alarmRepository != nil {
		writer = alarmRepository
	}

	service := &AlarmService{
		cache:           cacheClient,
		holodex:         holodexService,
		chzzk:           chzzkClient,
		twitch:          twitchClient,
		memberData:      memberData,
		alarmRepository: alarmRepository,
		alarmWriter:     writer,
		logger:          logger,
		targetPolicy:    targetPolicy,
	}
	memberDataFn := func() domain.MemberDataProvider { return service.memberData }
	service.cacheState = alarmcache.NewState(cacheClient, memberDataFn, logger)
	service.platformMapper = platformmap.NewMapper(cacheClient, memberDataFn, logger)

	return service, nil
}

func (as *AlarmService) getTargetMinutes() []int {
	as.targetMinutesMu.RLock()
	defer as.targetMinutesMu.RUnlock()

	return as.targetPolicy.Clone()
}

func (as *AlarmService) GetTargetMinutes() []int {
	return as.getTargetMinutes()
}

func (as *AlarmService) UpdateAlarmAdvanceMinutes(_ context.Context, alarmAdvanceMinutes int) []int {
	normalized := sharedchecker.NewTargetMinutePolicyFromRuntimeAdvance(alarmAdvanceMinutes)

	as.targetMinutesMu.Lock()
	as.targetPolicy = normalized
	as.targetMinutesMu.Unlock()

	if as.logger != nil {
		as.logger.Info("Alarm advance minutes updated",
			slog.Int("alarm_advance_minutes", alarmAdvanceMinutes),
			slog.Any("target_minutes", normalized.Clone()),
		)
	}

	return normalized.Clone()
}

func (as *AlarmService) Close(_ context.Context) error {
	if as == nil {
		return nil
	}
	return nil
}
