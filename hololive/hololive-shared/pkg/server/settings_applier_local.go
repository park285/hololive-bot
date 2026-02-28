package server

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

// localSettingsApplier: Bot 프로세스 내 직접 설정 적용 (in-process)
type localSettingsApplier struct {
	youtube             *youtube.Service
	holodex             *holodex.Service
	scraperProxyToggler ScraperProxyToggler
	alarm               domain.AlarmCRUD
}

var _ SettingsApplier = (*localSettingsApplier)(nil)

// NewLocalSettingsApplier: Bot 프로세스용 SettingsApplier를 생성합니다.
func NewLocalSettingsApplier(
	youtubeSvc *youtube.Service,
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
func (a *localSettingsApplier) ApplyScraperProxy(_ context.Context, enabled bool) map[string]any {
	runtime := map[string]any{
		"requested": enabled,
	}

	if a.youtube != nil {
		applied := a.youtube.SetScraperProxyEnabled(enabled)
		runtime["youtube_applied"] = applied
		runtime["youtube_enabled"] = a.youtube.ScraperProxyEnabled()
	}
	if a.holodex != nil {
		applied := a.holodex.SetScraperProxyEnabled(enabled)
		runtime["holodex_applied"] = applied
		runtime["holodex_enabled"] = a.holodex.ScraperProxyEnabled()
	}
	if a.scraperProxyToggler != nil {
		applied := a.scraperProxyToggler.SetProxyEnabled(enabled)
		schedulerEnabled, known := a.scraperProxyToggler.ProxyEnabled()
		runtime["scheduler_pollers_applied"] = applied
		runtime["scheduler_enabled"] = schedulerEnabled
		runtime["scheduler_known"] = known
	}

	return runtime
}

// ApplyAlarmAdvanceMinutes: 알람 사전 알림 시간을 직접 적용합니다.
func (a *localSettingsApplier) ApplyAlarmAdvanceMinutes(_ context.Context, minutes int) map[string]any {
	runtime := map[string]any{
		"alarm_requested_advance_minutes": minutes,
	}

	if a.alarm == nil {
		runtime["alarm_applied"] = false
		runtime["alarm_reason"] = "alarm service not configured"
		return runtime
	}

	targetMinutes := a.alarm.UpdateAlarmAdvanceMinutes(minutes)
	runtime["alarm_applied"] = true
	runtime["alarm_target_minutes"] = targetMinutes

	return runtime
}

// ApplyMajorEventScrapeHour: bot 프로세스에서는 미지원(LLM scheduler 전용) 설정입니다.
func (a *localSettingsApplier) ApplyMajorEventScrapeHour(_ context.Context, hourKST int) map[string]any {
	return map[string]any{
		"requested_hour_kst": hourKST,
		"applied":            false,
		"reason":             "llm scheduler settings are not available in local mode",
	}
}

// ApplyMajorEventScrapeRunNow: bot 프로세스에서는 미지원(LLM scheduler 전용) 설정입니다.
func (a *localSettingsApplier) ApplyMajorEventScrapeRunNow(_ context.Context) map[string]any {
	return map[string]any{
		"applied": false,
		"reason":  "llm scheduler settings are not available in local mode",
	}
}

// ApplyMemberNewsWeeklyRunNow: bot 프로세스에서는 미지원(LLM scheduler 전용) 설정입니다.
func (a *localSettingsApplier) ApplyMemberNewsWeeklyRunNow(_ context.Context) map[string]any {
	return map[string]any{
		"applied": false,
		"reason":  "llm scheduler settings are not available in local mode",
	}
}

// ScraperProxyRuntimeState: 현재 프록시 런타임 상태를 반환합니다.
func (a *localSettingsApplier) ScraperProxyRuntimeState(requested bool) map[string]any {
	runtime := map[string]any{
		"requested": requested,
	}

	if a.youtube != nil {
		runtime["youtube_enabled"] = a.youtube.ScraperProxyEnabled()
	}
	if a.holodex != nil {
		runtime["holodex_enabled"] = a.holodex.ScraperProxyEnabled()
	}
	if a.scraperProxyToggler != nil {
		schedulerEnabled, known := a.scraperProxyToggler.ProxyEnabled()
		runtime["scheduler_enabled"] = schedulerEnabled
		runtime["scheduler_known"] = known
	}
	if a.alarm != nil {
		runtime["alarm_target_minutes"] = a.alarm.GetTargetMinutes()
	}

	return runtime
}
