package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

type testSettingsApplier struct{}

func (testSettingsApplier) ApplyScraperProxy(_ context.Context, enabled bool) sharedsettings.ScraperProxyApplyResult {
	return sharedsettings.ScraperProxyApplyResult{Requested: enabled}
}

func (testSettingsApplier) ApplyAlarmAdvanceMinutes(_ context.Context, minutes int) sharedsettings.AlarmAdvanceMinutesApplyResult {
	return sharedsettings.AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes,
		AlarmApplied:                 true,
		AlarmTargetMinutes:           []int{minutes},
	}
}

func (testSettingsApplier) ApplyMemberNewsWeeklyRunNow(_ context.Context) sharedsettings.MemberNewsWeeklyRunNowResult {
	return sharedsettings.MemberNewsWeeklyRunNowResult{Applied: true}
}

func (testSettingsApplier) ScraperProxyRuntimeState(requested bool) sharedsettings.ScraperProxyRuntimeStateResult {
	return sharedsettings.ScraperProxyRuntimeStateResult{Requested: requested}
}

type testActivityLogger struct{}

func (testActivityLogger) Log(string, string, map[string]any) {}

type recordingConfigPublisher struct {
	scraperCalls []bool
	alarmCalls   []int
	failScraper  error
	failAlarm    error
}

func (p *recordingConfigPublisher) PublishScraperProxy(_ context.Context, enabled bool) error {
	p.scraperCalls = append(p.scraperCalls, enabled)
	return p.failScraper
}

func (p *recordingConfigPublisher) PublishAlarmAdvanceMinutes(_ context.Context, minutes int) error {
	p.alarmCalls = append(p.alarmCalls, minutes)
	return p.failAlarm
}

func newSettingsTestContext(t *testing.T, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/holo/settings", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx, rec
}

func decodeSettingsResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}

func TestSettingsHandler_UpdateSettings_PublishesConfigUpdates(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	settingsSvc := settingssvc.NewSettingsService(filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}, newDiscardLogger())
	publisher := &recordingConfigPublisher{}

	handler := &SettingsHandler{
		Logger:          newDiscardLogger(),
		Activity:        testActivityLogger{},
		Settings:        settingsSvc,
		ConfigPublisher: publisher,
		SettingsApplier: testSettingsApplier{},
	}

	ctx, rec := newSettingsTestContext(t, []byte(`{"alarmAdvanceMinutes":7,"scraperProxyEnabled":true}`))
	handler.UpdateSettings(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(publisher.scraperCalls) != 1 || !publisher.scraperCalls[0] {
		t.Fatalf("scraper publish calls=%v", publisher.scraperCalls)
	}
	if len(publisher.alarmCalls) != 1 || publisher.alarmCalls[0] != 7 {
		t.Fatalf("alarm publish calls=%v", publisher.alarmCalls)
	}

	payload := decodeSettingsResponse(t, rec)
	runtime, ok := payload["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime payload missing: %#v", payload["runtime"])
	}
	if got := runtime["config_publish_scraper_proxy"]; got != true {
		t.Fatalf("config_publish_scraper_proxy=%v want=true", got)
	}
	if got := runtime["config_publish_alarm_advance_minutes"]; got != true {
		t.Fatalf("config_publish_alarm_advance_minutes=%v want=true", got)
	}
}

func TestSettingsHandler_UpdateSettings_ReportsPublishFailure(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	settingsSvc := settingssvc.NewSettingsService(filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}, newDiscardLogger())
	publisher := &recordingConfigPublisher{
		failScraper: fmt.Errorf("scraper publish failed"),
		failAlarm:   fmt.Errorf("alarm publish failed"),
	}

	handler := &SettingsHandler{
		Logger:          newDiscardLogger(),
		Activity:        testActivityLogger{},
		Settings:        settingsSvc,
		ConfigPublisher: publisher,
		SettingsApplier: testSettingsApplier{},
	}

	ctx, rec := newSettingsTestContext(t, []byte(`{"alarmAdvanceMinutes":9,"scraperProxyEnabled":true}`))
	handler.UpdateSettings(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	payload := decodeSettingsResponse(t, rec)
	runtime, ok := payload["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime payload missing: %#v", payload["runtime"])
	}
	if got := runtime["config_publish_scraper_proxy"]; got != false {
		t.Fatalf("config_publish_scraper_proxy=%v want=false", got)
	}
	if got := runtime["config_publish_alarm_advance_minutes"]; got != false {
		t.Fatalf("config_publish_alarm_advance_minutes=%v want=false", got)
	}
}
