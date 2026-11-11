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

package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

type stubSettingsApplier struct{}

func (s *stubSettingsApplier) ApplyAlarmAdvanceMinutes(_ context.Context, minutes int) (adminsettings.AlarmAdvanceMinutesApplyResult, error) {
	return adminsettings.AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes,
		AlarmApplied:                 true,
	}, nil
}

func (s *stubSettingsApplier) SettingsRuntimeState() adminsettings.SettingsRuntimeStateResult {
	return adminsettings.SettingsRuntimeStateResult{AlarmTargetMinutes: []int{5}}
}

const settingsPath = "/api/holo/settings"

func TestSettingsAPIHandler_BasicBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid json branches", settingsInvalidJSONBranches)
	t.Run("get settings and update success", settingsGetAndUpdateSuccess)
}

func settingsInvalidJSONBranches(t *testing.T) {
	handler := newSettingsHandler(Deps{
		Logger: newDiscardLogger(),
	})

	tests := []struct {
		method string
		path   string
		invoke gin.HandlerFunc
	}{
		{http.MethodPost, "/api/holo/settings/room-name", handler.SetRoomName},
		{http.MethodPatch, settingsPath, handler.UpdateSettings},
	}

	for _, tt := range tests {
		ctx, rec := newAPITestContext(tt.method, tt.path, []byte("{"))
		tt.invoke(ctx)

		assertErrorResponse(t, rec, http.StatusBadRequest, "invalid request body")
	}
}

func settingsGetAndUpdateSuccess(t *testing.T) {
	applier := &stubSettingsApplier{}
	settingsService := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settings.Settings{
		AlarmAdvanceMinutes: 5,
	})

	handler := newSettingsHandler(Deps{
		Logger:   newDiscardLogger(),
		Activity: newActivityLoggerForTest(t),
		Settings: settingsService,
		Applier:  applier,
	})

	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
		invoke gin.HandlerFunc
	}{
		{"GetSettings", http.MethodGet, settingsPath, nil, handler.GetSettings},
		{"UpdateSettings", http.MethodPatch, settingsPath, []byte(`{"alarmAdvanceMinutes":7}`), handler.UpdateSettings},
	}

	for _, tt := range tests {
		ctx, rec := newAPITestContext(tt.method, tt.path, tt.body)
		tt.invoke(ctx)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d want=%d body=%s", tt.name, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
}
