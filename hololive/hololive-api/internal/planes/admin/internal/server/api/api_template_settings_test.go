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

	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type stubSettingsApplier struct {
	memberNewsApplied bool
}

func (s *stubSettingsApplier) ApplyScraperProxy(_ context.Context, enabled bool) sharedsettings.ScraperProxyApplyResult {
	return sharedsettings.ScraperProxyApplyResult{
		Requested: enabled,
		Reason:    "test",
	}
}

func (s *stubSettingsApplier) ApplyAlarmAdvanceMinutes(_ context.Context, minutes int) sharedsettings.AlarmAdvanceMinutesApplyResult {
	return sharedsettings.AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes,
		AlarmApplied:                 true,
	}
}

func (s *stubSettingsApplier) ApplyMemberNewsWeeklyRunNow(_ context.Context) sharedsettings.MemberNewsWeeklyRunNowResult {
	s.memberNewsApplied = true
	return sharedsettings.MemberNewsWeeklyRunNowResult{Applied: true, Source: "test"}
}

func (s *stubSettingsApplier) ScraperProxyRuntimeState(requested bool) sharedsettings.ScraperProxyRuntimeStateResult {
	return sharedsettings.ScraperProxyRuntimeStateResult{
		Requested: requested,
		Reason:    "test",
	}
}

const (
	templateKeyParam       = "key"
	templateInvalidKey     = "invalid"
	templateInvalidKeyPath = "/api/holo/templates/invalid"
	settingsPath           = "/api/holo/settings"
	settingsLLMPath        = "/api/holo/settings/llm"
)

type templateValidationCase struct {
	name       string
	method     string
	path       string
	body       []byte
	paramKey   string
	paramValue string
	invoke     gin.HandlerFunc
	wantStatus int
}

func TestTemplateHandler_ValidationBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &TemplateHandler{Handler: &Handler{
		templateAdmin: &template.AdminService{},
		logger:        newDiscardLogger(),
	}}

	previewPath := templateInvalidKeyPath + "/preview"

	tests := []templateValidationCase{
		{"get by key invalid key", http.MethodGet, templateInvalidKeyPath, nil, templateKeyParam, templateInvalidKey, handler.GetTemplateByKey, http.StatusNotFound},
		{"upsert invalid json", http.MethodPut, templateInvalidKeyPath, []byte("{"), templateKeyParam, templateInvalidKey, handler.UpsertTemplate, http.StatusBadRequest},
		{"upsert invalid key", http.MethodPut, templateInvalidKeyPath, []byte(`{"body":"hello"}`), templateKeyParam, templateInvalidKey, handler.UpsertTemplate, http.StatusNotFound},
		{"delete override missing channel id", http.MethodDelete, templateInvalidKeyPath, nil, templateKeyParam, templateInvalidKey, handler.DeleteTemplateOverride, http.StatusBadRequest},
		{"preview invalid json", http.MethodPost, previewPath, []byte("{"), templateKeyParam, templateInvalidKey, handler.PreviewTemplate, http.StatusBadRequest},
		{"preview invalid key", http.MethodPost, previewPath, []byte(`{"body":"hello"}`), templateKeyParam, templateInvalidKey, handler.PreviewTemplate, http.StatusNotFound},
		{"get revision invalid id", http.MethodGet, "/api/holo/templates/revisions/abc", nil, "id", "abc", handler.GetTemplateRevision, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, rec := newAPITestContext(tt.method, tt.path, tt.body)

			ctx.Params = gin.Params{{Key: tt.paramKey, Value: tt.paramValue}}
			tt.invoke(ctx)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestSettingsAPIHandler_BasicBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid json branches", settingsInvalidJSONBranches)
	t.Run("get logs/settings and update success", settingsGetAndUpdateSuccess)
}

func settingsInvalidJSONBranches(t *testing.T) {
	handler := &SettingsAPIHandler{Handler: &Handler{
		logger: newDiscardLogger(),
	}}

	tests := []struct {
		method string
		path   string
		invoke gin.HandlerFunc
	}{
		{http.MethodPost, "/api/holo/settings/room-name", handler.SetRoomName},
		{http.MethodPost, "/api/holo/settings/user-name", handler.SetUserName},
		{http.MethodPatch, settingsPath, handler.UpdateSettings},
		{http.MethodPatch, settingsLLMPath, handler.UpdateLLMSettings},
	}

	for _, tt := range tests {
		ctx, rec := newAPITestContext(tt.method, tt.path, []byte("{"))
		tt.invoke(ctx)

		assertErrorResponse(t, rec, http.StatusBadRequest, "invalid request body")
	}
}

func settingsGetAndUpdateSuccess(t *testing.T) {
	applier := &stubSettingsApplier{}
	settingsService := settings.NewSettingsService(filepath.Join(t.TempDir(), "settings.json"), settings.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}, newDiscardLogger())

	handler := &SettingsAPIHandler{Handler: &Handler{
		logger:          newDiscardLogger(),
		activity:        newActivityLoggerForTest(t),
		settings:        settingsService,
		settingsApplier: applier,
	}}

	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
		invoke gin.HandlerFunc
	}{
		{"GetLogs", http.MethodGet, "/api/holo/settings/logs", nil, handler.GetLogs},
		{"GetSettings", http.MethodGet, settingsPath, nil, handler.GetSettings},
		{"UpdateSettings", http.MethodPatch, settingsPath, []byte(`{"alarmAdvanceMinutes":7,"scraperProxyEnabled":true}`), handler.UpdateSettings},
		{"UpdateLLMSettings", http.MethodPatch, settingsLLMPath, []byte(`{"memberNewsWeeklyRunNow":true}`), handler.UpdateLLMSettings},
	}

	for _, tt := range tests {
		ctx, rec := newAPITestContext(tt.method, tt.path, tt.body)
		tt.invoke(ctx)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d want=%d body=%s", tt.name, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	if !applier.memberNewsApplied {
		t.Fatal("ApplyMemberNewsWeeklyRunNow should be called")
	}
}
