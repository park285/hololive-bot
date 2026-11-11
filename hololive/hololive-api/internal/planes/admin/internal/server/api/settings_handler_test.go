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
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
	"github.com/kapu/hololive-api/internal/service/activity"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

type testSettingsApplier struct{}

func (testSettingsApplier) ApplyAlarmAdvanceMinutes(_ context.Context, minutes int) (adminsettings.AlarmAdvanceMinutesApplyResult, error) {
	return adminsettings.AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes,
		AlarmApplied:                 true,
		AlarmTargetMinutes:           []int{minutes},
	}, nil
}

func (testSettingsApplier) SettingsRuntimeState() adminsettings.SettingsRuntimeStateResult {
	return adminsettings.SettingsRuntimeStateResult{}
}

type testActivityLogger struct{}

func (testActivityLogger) Log(string, string, map[string]any) {}

type roomNameRecordingAlarm struct {
	stubAlarmCRUDForServer

	calls []string
}

func (a *roomNameRecordingAlarm) SetRoomName(_ context.Context, roomID, roomName string) error {
	a.calls = append(a.calls, roomID+"|"+roomName)

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
	})

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

func TestSettingsHandler_SetRoomName_BlankNameClearsBoundsAndMissingNameIsRejected(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	maxRoomID := strings.Repeat("r", contractsalarm.MaxRoomIDLength)
	maxRoomName := strings.Repeat("가", contractsalarm.MaxRoomNameLength)

	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantCalls   []string
		wantMessage string
	}{
		{name: "set", body: `{"roomId":" room-1 ","roomName":" 관리 방 "}`, wantStatus: http.StatusOK, wantCalls: []string{"room-1|관리 방"}, wantMessage: "Room name set successfully"},
		{name: "blank clears", body: `{"roomId":"room-1","roomName":"  "}`, wantStatus: http.StatusOK, wantCalls: []string{"room-1|"}, wantMessage: "Room name cleared"},
		{name: "bounds accepted", body: `{"roomId":"` + maxRoomID + `","roomName":"` + maxRoomName + `"}`, wantStatus: http.StatusOK, wantCalls: []string{maxRoomID + "|" + maxRoomName}, wantMessage: "Room name set successfully"},
		{name: "name over 255", body: `{"roomId":"room-1","roomName":"` + maxRoomName + `가"}`, wantStatus: http.StatusBadRequest},
		{name: "room id over 100", body: `{"roomId":"` + maxRoomID + `r","roomName":"관리 방"}`, wantStatus: http.StatusBadRequest},
		{name: "blank room id", body: `{"roomId":"  ","roomName":"관리 방"}`, wantStatus: http.StatusBadRequest},
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

func TestSettingsHandler_SetRoomName_LogsMaskedRoomID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	const rawRoomID = "private-room-slug"

	for _, body := range []string{
		`{"roomId":"` + rawRoomID + `","roomName":"관리 방"}`,
		`{"roomId":"` + rawRoomID + `","roomName":" "}`,
	} {
		var logs bytes.Buffer

		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		// 운영 배선과 같은 stdout 전용 activity logger라 activity 줄도 같은 버퍼에서 함께 검사한다.
		handler := &SettingsHandler{
			Logger:   logger,
			Activity: activity.NewActivityLogger(logger),
			Alarm:    &roomNameRecordingAlarm{},
		}

		ctx, rec := newSettingsTestContext(t, []byte(body))
		handler.SetRoomName(ctx)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		if !strings.Contains(logs.String(), `"room_id":`) {
			t.Fatalf("room name log has no room_id attribute: %s", logs.String())
		}

		if !strings.Contains(logs.String(), `"msg":"activity"`) {
			t.Fatalf("room name change wrote no activity line: %s", logs.String())
		}

		if strings.Contains(logs.String(), rawRoomID) {
			t.Fatalf("room name log exposed raw room id: %s", logs.String())
		}
	}
}

func mustNewTestSettingsService(t *testing.T, filePath string, defaults settingssvc.Settings) *adminsettings.Service {
	t.Helper()

	service, err := adminsettings.NewSettingsService(filePath, defaults)
	if err != nil {
		t.Fatalf("NewSettingsService() error = %v", err)
	}

	return service
}
