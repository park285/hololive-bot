package api

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
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

// roomNameRecordingAlarm은 관리자 방 이름 요청이 알림 서비스에 넘긴 이름을 기록한다.
type roomNameRecordingAlarm struct {
	stubAlarmCRUDForServer

	calls []string
}

func (a *roomNameRecordingAlarm) SetRoomName(_ context.Context, _, roomName string) error {
	a.calls = append(a.calls, roomName)

	return nil
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

func TestSettingsHandler_UpdateSettings_RejectsInvalidAlarmAdvanceMinutes(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	settingsService := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{
		AlarmAdvanceMinutes: 5,
	}, newDiscardLogger())

	handler := &SettingsHandler{
		Logger:          newDiscardLogger(),
		Activity:        testActivityLogger{},
		Settings:        settingsService,
		SettingsApplier: testSettingsApplier{},
	}

	ctx, rec := newSettingsTestContext(t, []byte(`{"alarmAdvanceMinutes":-1}`))
	handler.UpdateSettings(ctx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	if got := settingsService.Get().AlarmAdvanceMinutes; got != 5 {
		t.Fatalf("AlarmAdvanceMinutes=%d want unchanged 5", got)
	}
}

// roomName 필드는 필수지만 공백뿐인 값은 관리자 지정 이름 해제 요청이다.
func TestSettingsHandler_SetRoomName_BlankNameClearsAndMissingNameIsRejected(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantCalls   []string
		wantMessage string
	}{
		{name: "set", body: `{"roomId":"room-1","roomName":" 관리 방 "}`, wantStatus: http.StatusOK, wantCalls: []string{"관리 방"}, wantMessage: "Room name set successfully"},
		{name: "blank clears", body: `{"roomId":"room-1","roomName":"  "}`, wantStatus: http.StatusOK, wantCalls: []string{""}, wantMessage: "Room name cleared"},
		{name: "missing name", body: `{"roomId":"room-1"}`, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			alarm := &roomNameRecordingAlarm{}
			handler := &SettingsHandler{Logger: newDiscardLogger(), Activity: testActivityLogger{}, Alarm: alarm}

			ctx, rec := newSettingsTestContext(t, []byte(tt.body))
			handler.SetRoomName(ctx)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if !slices.Equal(alarm.calls, tt.wantCalls) {
				t.Fatalf("SetRoomName calls=%q want=%q", alarm.calls, tt.wantCalls)
			}

			if tt.wantMessage != "" {
				if got := decodeSettingsResponse(t, rec)["message"]; got != tt.wantMessage {
					t.Fatalf("message=%v want=%q", got, tt.wantMessage)
				}
			}
		})
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
