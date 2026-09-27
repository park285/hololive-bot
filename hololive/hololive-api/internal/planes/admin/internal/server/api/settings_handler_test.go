package api

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

type testSettingsApplier struct{}

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

func (testSettingsApplier) SettingsRuntimeState() sharedsettings.SettingsRuntimeStateResult {
	return sharedsettings.SettingsRuntimeStateResult{}
}

type testActivityLogger struct{}

func (testActivityLogger) Log(string, string, map[string]any) {}

type recordingConfigPublisher struct {
	alarmCalls []int
	failAlarm  error
}

func (p *recordingConfigPublisher) PublishAlarmAdvanceMinutes(_ context.Context, minutes int) error {
	p.alarmCalls = append(p.alarmCalls, minutes)
	return p.failAlarm
}

func newSettingsTestContext(t *testing.T, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)

	ctx.Request = httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/holo/settings", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	return ctx, rec
}

func decodeSettingsResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	return payload
}

func TestSettingsHandler_UpdateSettings_PublishesConfigUpdates(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	settingsService := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{
		AlarmAdvanceMinutes: 5,
	}, newDiscardLogger())
	publisher := &recordingConfigPublisher{}

	handler := &SettingsHandler{
		Logger:          newDiscardLogger(),
		Activity:        testActivityLogger{},
		Settings:        settingsService,
		ConfigPublisher: publisher,
		SettingsApplier: testSettingsApplier{},
	}

	ctx, rec := newSettingsTestContext(t, []byte(`{"alarmAdvanceMinutes":7}`))
	handler.UpdateSettings(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if len(publisher.alarmCalls) != 1 || publisher.alarmCalls[0] != 7 {
		t.Fatalf("alarm publish calls=%v", publisher.alarmCalls)
	}

	if got := settingsService.Get().TargetMinutes; len(got) != 3 || got[0] != 7 || got[1] != 3 || got[2] != 1 {
		t.Fatalf("persisted target minutes=%v want=[7 3 1]", got)
	}

	payload := decodeSettingsResponse(t, rec)
	runtime, ok := payload["runtime"].(map[string]any)

	if !ok {
		t.Fatalf("runtime payload missing: %#v", payload["runtime"])
	}

	// 퇴역한 scraper proxy 발행 결과 키는 다시 나오지 않는다(DEC-20260926-hololive-legacy-env-config-retirement).
	if _, exists := runtime["config_publish_scraper_proxy"]; exists {
		t.Fatalf("retired config_publish_scraper_proxy key present: %#v", runtime)
	}

	if got := runtime["config_publish_alarm_advance_minutes"]; got != true {
		t.Fatalf("config_publish_alarm_advance_minutes=%v want=true", got)
	}
}

func TestSettingsHandler_UpdateSettings_ReportsPublishFailure(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	settingsService := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{
		AlarmAdvanceMinutes: 5,
	}, newDiscardLogger())
	publisher := &recordingConfigPublisher{
		failAlarm: errors.New("alarm publish failed"),
	}

	handler := &SettingsHandler{
		Logger:          newDiscardLogger(),
		Activity:        testActivityLogger{},
		Settings:        settingsService,
		ConfigPublisher: publisher,
		SettingsApplier: testSettingsApplier{},
	}

	ctx, rec := newSettingsTestContext(t, []byte(`{"alarmAdvanceMinutes":9}`))
	handler.UpdateSettings(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	payload := decodeSettingsResponse(t, rec)
	runtime, ok := payload["runtime"].(map[string]any)

	if !ok {
		t.Fatalf("runtime payload missing: %#v", payload["runtime"])
	}

	if got := runtime["config_publish_alarm_advance_minutes"]; got != false {
		t.Fatalf("config_publish_alarm_advance_minutes=%v want=false", got)
	}
}

func TestSettingsHandler_UpdateSettings_RejectsInvalidAlarmAdvanceMinutes(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	settingsService := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{
		AlarmAdvanceMinutes: 5,
	}, newDiscardLogger())
	publisher := &recordingConfigPublisher{}

	handler := &SettingsHandler{
		Logger:          newDiscardLogger(),
		Activity:        testActivityLogger{},
		Settings:        settingsService,
		ConfigPublisher: publisher,
		SettingsApplier: testSettingsApplier{},
	}

	ctx, rec := newSettingsTestContext(t, []byte(`{"alarmAdvanceMinutes":-1}`))
	handler.UpdateSettings(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	if len(publisher.alarmCalls) != 0 {
		t.Fatalf("invalid settings must not publish config updates: alarm=%v", publisher.alarmCalls)
	}

	if got := settingsService.Get().AlarmAdvanceMinutes; got != 5 {
		t.Fatalf("AlarmAdvanceMinutes=%d want unchanged 5", got)
	}
}

func mustNewTestSettingsService(t *testing.T, filePath string, defaults settingssvc.Settings, logger *slog.Logger) *settingssvc.Service {
	t.Helper()

	service, err := settingssvc.NewSettingsService(filePath, defaults, logger)
	if err != nil {
		t.Fatalf("NewSettingsService() error = %v", err)
	}

	return service
}
