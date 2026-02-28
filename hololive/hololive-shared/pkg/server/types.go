package server

import "context"

// ScraperProxyToggler: 스크래퍼 스케줄러 프록시 토글 인터페이스
type ScraperProxyToggler interface {
	SetProxyEnabled(enabled bool) int
	ProxyEnabled() (enabled bool, known bool)
}

// SettingsApplier: 설정 변경을 런타임에 적용하는 인터페이스
type SettingsApplier interface {
	ApplyScraperProxy(ctx context.Context, enabled bool) map[string]any
	ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) map[string]any
	ApplyMajorEventScrapeHour(ctx context.Context, hourKST int) map[string]any
	ApplyMajorEventScrapeRunNow(ctx context.Context) map[string]any
	ApplyMemberNewsWeeklyRunNow(ctx context.Context) map[string]any
	ScraperProxyRuntimeState(requested bool) map[string]any
}
