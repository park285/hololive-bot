package server

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-admin/internal/service/configpub"
)

// pubsubSettingsApplier: admin-api 프로세스에서 Valkey Pub/Sub를 통해 설정 변경을 전파
type pubsubSettingsApplier struct {
	publisher *configpub.Publisher
	alarm     domain.AlarmCRUD
	logger    *slog.Logger
}

var _ SettingsApplier = (*pubsubSettingsApplier)(nil)

// NewPubSubSettingsApplier: admin-api용 SettingsApplier를 생성합니다.
func NewPubSubSettingsApplier(
	publisher *configpub.Publisher,
	alarm domain.AlarmCRUD,
	logger *slog.Logger,
) SettingsApplier {
	return &pubsubSettingsApplier{
		publisher: publisher,
		alarm:     alarm,
		logger:    logger,
	}
}

// ApplyScraperProxy: Pub/Sub로 scraper_proxy 변경을 발행합니다.
func (a *pubsubSettingsApplier) ApplyScraperProxy(ctx context.Context, enabled bool) map[string]any {
	runtime := map[string]any{
		"requested": enabled,
	}

	payload, err := json.Marshal(struct {
		Enabled bool `json:"enabled"`
	}{Enabled: enabled})
	if err != nil {
		a.logger.Error("Failed to marshal scraper_proxy payload", slog.Any("error", err))
		runtime["marshal_error"] = err.Error()
		return runtime
	}

	err = a.publisher.Publish(ctx, configsub.ConfigUpdate{
		Type:    "scraper_proxy",
		Payload: payload,
	})
	if err != nil {
		a.logger.Error("Failed to publish scraper_proxy update", slog.Any("error", err))
		runtime["publish_error"] = err.Error()
	} else {
		runtime["published"] = true
	}

	return runtime
}

// ApplyAlarmAdvanceMinutes: Pub/Sub로 alarm_advance_minutes 변경을 발행하고, alarm-dispatcher HTTP 클라이언트에도 직접 전달합니다.
func (a *pubsubSettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) map[string]any {
	runtime := map[string]any{
		"alarm_requested_advance_minutes": minutes,
	}

	// Pub/Sub 발행 (Bot이 수신하여 로컬 적용)
	payload, err := json.Marshal(struct {
		Minutes int `json:"minutes"`
	}{Minutes: minutes})
	if err != nil {
		a.logger.Error("Failed to marshal alarm_advance_minutes payload", slog.Any("error", err))
		runtime["marshal_error"] = err.Error()
		return runtime
	}

	err = a.publisher.Publish(ctx, configsub.ConfigUpdate{
		Type:    "alarm_advance_minutes",
		Payload: payload,
	})
	if err != nil {
		a.logger.Error("Failed to publish alarm_advance_minutes update", slog.Any("error", err))
		runtime["publish_error"] = err.Error()
	} else {
		runtime["published"] = true
	}

	// alarm-dispatcher HTTP 클라이언트를 통한 직접 적용
	if a.alarm != nil {
		targetMinutes := a.alarm.UpdateAlarmAdvanceMinutes(minutes)
		runtime["alarm_applied"] = true
		runtime["alarm_target_minutes"] = targetMinutes
	} else {
		runtime["alarm_applied"] = false
		runtime["alarm_reason"] = "alarm service not configured"
	}

	return runtime
}

// ApplyMajorEventScrapeHour: Pub/Sub로 major event scrape 시각 변경을 발행합니다.
func (a *pubsubSettingsApplier) ApplyMajorEventScrapeHour(ctx context.Context, hourKST int) map[string]any {
	runtime := map[string]any{
		"requested_hour_kst": hourKST,
	}

	payload, err := json.Marshal(struct {
		HourKST int `json:"hour_kst"`
	}{HourKST: hourKST})
	if err != nil {
		a.logger.Error("Failed to marshal majorevent_scrape_hour_kst payload", slog.Any("error", err))
		runtime["marshal_error"] = err.Error()
		return runtime
	}

	err = a.publisher.Publish(ctx, configsub.ConfigUpdate{
		Type:    "majorevent_scrape_hour_kst",
		Payload: payload,
	})
	if err != nil {
		a.logger.Error("Failed to publish majorevent_scrape_hour_kst update", slog.Any("error", err))
		runtime["publish_error"] = err.Error()
	} else {
		runtime["published"] = true
	}

	return runtime
}

// ApplyMajorEventScrapeRunNow: Pub/Sub로 major event 즉시 스크래핑 실행 요청을 발행합니다.
func (a *pubsubSettingsApplier) ApplyMajorEventScrapeRunNow(ctx context.Context) map[string]any {
	runtime := map[string]any{}

	payload, err := json.Marshal(struct{}{})
	if err != nil {
		a.logger.Error("Failed to marshal majorevent_scrape_run_now payload", slog.Any("error", err))
		runtime["marshal_error"] = err.Error()
		return runtime
	}

	err = a.publisher.Publish(ctx, configsub.ConfigUpdate{
		Type:    "majorevent_scrape_run_now",
		Payload: payload,
	})
	if err != nil {
		a.logger.Error("Failed to publish majorevent_scrape_run_now update", slog.Any("error", err))
		runtime["publish_error"] = err.Error()
	} else {
		runtime["published"] = true
	}

	return runtime
}

// ApplyMemberNewsWeeklyRunNow: Pub/Sub로 membernews 주간 다이제스트 즉시 실행 요청을 발행합니다.
func (a *pubsubSettingsApplier) ApplyMemberNewsWeeklyRunNow(ctx context.Context) map[string]any {
	runtime := map[string]any{}

	payload, err := json.Marshal(struct{}{})
	if err != nil {
		a.logger.Error("Failed to marshal membernews_weekly_run_now payload", slog.Any("error", err))
		runtime["marshal_error"] = err.Error()
		return runtime
	}

	err = a.publisher.Publish(ctx, configsub.ConfigUpdate{
		Type:    "membernews_weekly_run_now",
		Payload: payload,
	})
	if err != nil {
		a.logger.Error("Failed to publish membernews_weekly_run_now update", slog.Any("error", err))
		runtime["publish_error"] = err.Error()
	} else {
		runtime["published"] = true
	}

	return runtime
}

// ScraperProxyRuntimeState: admin-api에서는 로컬 YouTube/Holodex 참조가 없으므로 최소 정보를 반환합니다.
func (a *pubsubSettingsApplier) ScraperProxyRuntimeState(requested bool) map[string]any {
	runtime := map[string]any{
		"requested": requested,
		"mode":      "pubsub",
	}

	if a.alarm != nil {
		runtime["alarm_target_minutes"] = a.alarm.GetTargetMinutes()
	}

	return runtime
}
