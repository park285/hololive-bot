package settings

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

// localSettingsApplier: Bot 프로세스 내 직접 설정 적용 (in-process)
type localSettingsApplier struct {
	youtube             youtube.Service
	holodex             *holodex.Service
	scraperProxyToggler ScraperProxyToggler
	alarm               domain.AlarmCRUD
}

var _ SettingsApplier = (*localSettingsApplier)(nil)

// NewLocalSettingsApplier: Bot 프로세스용 SettingsApplier를 생성합니다.
func NewLocalSettingsApplier(
	youtubeSvc youtube.Service,
	holodexSvc *holodex.Service,
	scraperProxyToggler ScraperProxyToggler,
	alarm domain.AlarmCRUD,
) SettingsApplier {
	return &localSettingsApplier{
		youtube:             youtubeSvc,
		holodex:             holodexSvc,
		scraperProxyToggler: scraperProxyToggler,
		alarm:               alarm,
	}
}

// ApplyScraperProxy: YouTube/Holodex/스케줄러 프록시 설정을 직접 적용합니다.
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

// ApplyAlarmAdvanceMinutes: 알람 사전 알림 시간을 직접 적용합니다.
func (a *localSettingsApplier) ApplyAlarmAdvanceMinutes(_ context.Context, minutes int) AlarmAdvanceMinutesApplyResult {
	runtime := AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes,
	}

	if a.alarm == nil {
		runtime.AlarmApplied = false
		runtime.AlarmReason = "alarm service not configured"
		return runtime
	}

	targetMinutes := a.alarm.UpdateAlarmAdvanceMinutes(minutes)
	runtime.AlarmApplied = true
	runtime.AlarmTargetMinutes = targetMinutes

	return runtime
}

// ApplyMemberNewsWeeklyRunNow: bot 프로세스에서는 미지원(LLM scheduler 전용) 설정입니다.
func (a *localSettingsApplier) ApplyMemberNewsWeeklyRunNow(_ context.Context) MemberNewsWeeklyRunNowResult {
	return MemberNewsWeeklyRunNowResult{
		Applied: false,
		Reason:  "llm scheduler settings are not available in local mode",
	}
}

// ScraperProxyRuntimeState: 현재 프록시 런타임 상태를 반환합니다.
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
