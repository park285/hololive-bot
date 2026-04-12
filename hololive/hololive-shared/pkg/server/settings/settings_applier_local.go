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

package settings

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

// localSettingsApplier: Bot 프로세스 내 직접 설정 적용 (in-process)
type localSettingsApplier struct {
	youtube             youtube.Service
	holodex             *holodex.Service
	scraperProxyToggler *poller.Scheduler
	alarm               domain.AlarmCRUD
}

var _ SettingsApplier = (*localSettingsApplier)(nil)

func NewLocalSettingsApplier(
	youtubeSvc youtube.Service,
	holodexSvc *holodex.Service,
	scraperProxyToggler *poller.Scheduler,
	alarm domain.AlarmCRUD,
) SettingsApplier {
	return &localSettingsApplier{
		youtube:             youtubeSvc,
		holodex:             holodexSvc,
		scraperProxyToggler: scraperProxyToggler,
		alarm:               alarm,
	}
}

func (a *localSettingsApplier) ApplyScraperProxy(_ context.Context, enabled bool) ScraperProxyApplyResult {
	runtime := ScraperProxyApplyResult{
		Requested: enabled,
	}

	if a.youtube != nil {
		applied := a.youtube.SetScraperProxyEnabled(enabled)
		youtubeEnabled := a.youtube.ScraperProxyEnabled()
		runtime.YoutubeApplied = &applied
		runtime.YoutubeEnabled = &youtubeEnabled
	}
	if a.holodex != nil {
		applied := a.holodex.SetScraperProxyEnabled(enabled)
		holodexEnabled := a.holodex.ScraperProxyEnabled()
		runtime.HolodexApplied = &applied
		runtime.HolodexEnabled = &holodexEnabled
	}
	if a.scraperProxyToggler != nil {
		applied := a.scraperProxyToggler.SetProxyEnabled(enabled)
		schedulerEnabled, known := a.scraperProxyToggler.ProxyEnabled()
		runtime.SchedulerPollersApplied = &applied
		runtime.SchedulerEnabled = &schedulerEnabled
		runtime.SchedulerKnown = &known
	}

	return runtime
}

func (a *localSettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) AlarmAdvanceMinutesApplyResult {
	runtime := AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes,
	}

	if a.alarm == nil {
		runtime.AlarmApplied = false
		runtime.AlarmReason = "alarm service not configured"
		return runtime
	}

	targetMinutes := a.alarm.UpdateAlarmAdvanceMinutes(ctx, minutes)
	runtime.AlarmApplied = true
	runtime.AlarmTargetMinutes = targetMinutes

	return runtime
}

func (a *localSettingsApplier) ApplyMemberNewsWeeklyRunNow(_ context.Context) MemberNewsWeeklyRunNowResult {
	return MemberNewsWeeklyRunNowResult{
		Applied: false,
		Reason:  "llm scheduler settings are not available in local mode",
	}
}

func (a *localSettingsApplier) ScraperProxyRuntimeState(requested bool) ScraperProxyRuntimeStateResult {
	runtime := ScraperProxyRuntimeStateResult{
		Requested: requested,
	}

	if a.youtube != nil {
		youtubeEnabled := a.youtube.ScraperProxyEnabled()
		runtime.YoutubeEnabled = &youtubeEnabled
	}
	if a.holodex != nil {
		holodexEnabled := a.holodex.ScraperProxyEnabled()
		runtime.HolodexEnabled = &holodexEnabled
	}
	if a.scraperProxyToggler != nil {
		schedulerEnabled, known := a.scraperProxyToggler.ProxyEnabled()
		runtime.SchedulerEnabled = &schedulerEnabled
		runtime.SchedulerKnown = &known
	}
	if a.alarm != nil {
		runtime.AlarmTargetMinutes = a.alarm.GetTargetMinutes()
	}

	return runtime
}
