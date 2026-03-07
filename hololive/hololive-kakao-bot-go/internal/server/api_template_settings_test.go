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

package server

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

func TestTemplateAPIHandler_ValidationBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &TemplateAPIHandler{APIHandler: &APIHandler{
		templateAdmin: &template.AdminService{},
		logger:        newDiscardLogger(),
	}}

	t.Run("get by key invalid key", func(t *testing.T) {
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/templates/invalid", nil)
		ctx.Params = gin.Params{{Key: "key", Value: "invalid"}}
		handler.GetTemplateByKey(ctx)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})

	t.Run("upsert invalid json", func(t *testing.T) {
		ctx, rec := newAPITestContext(http.MethodPut, "/api/holo/templates/invalid", []byte("{"))
		ctx.Params = gin.Params{{Key: "key", Value: "invalid"}}
		handler.UpsertTemplate(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("upsert invalid key", func(t *testing.T) {
		ctx, rec := newAPITestContext(http.MethodPut, "/api/holo/templates/invalid", []byte(`{"body":"hello"}`))
		ctx.Params = gin.Params{{Key: "key", Value: "invalid"}}
		handler.UpsertTemplate(ctx)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})

	t.Run("delete override missing channel id", func(t *testing.T) {
		ctx, rec := newAPITestContext(http.MethodDelete, "/api/holo/templates/invalid", nil)
		ctx.Params = gin.Params{{Key: "key", Value: "invalid"}}
		handler.DeleteTemplateOverride(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("preview invalid json", func(t *testing.T) {
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/templates/invalid/preview", []byte("{"))
		ctx.Params = gin.Params{{Key: "key", Value: "invalid"}}
		handler.PreviewTemplate(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("preview invalid key", func(t *testing.T) {
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/templates/invalid/preview", []byte(`{"body":"hello"}`))
		ctx.Params = gin.Params{{Key: "key", Value: "invalid"}}
		handler.PreviewTemplate(ctx)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
	})

	t.Run("get revision invalid id", func(t *testing.T) {
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/templates/revisions/abc", nil)
		ctx.Params = gin.Params{{Key: "id", Value: "abc"}}
		handler.GetTemplateRevision(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})
}

func TestSettingsAPIHandler_BasicBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("invalid json branches", func(t *testing.T) {
		handler := &SettingsAPIHandler{APIHandler: &APIHandler{
			logger: newDiscardLogger(),
		}}

		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/settings/room-name", []byte("{"))
		handler.SetRoomName(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SetRoomName status=%d want=%d", rec.Code, http.StatusBadRequest)
		}

		ctx, rec = newAPITestContext(http.MethodPost, "/api/holo/settings/user-name", []byte("{"))
		handler.SetUserName(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SetUserName status=%d want=%d", rec.Code, http.StatusBadRequest)
		}

		ctx, rec = newAPITestContext(http.MethodPatch, "/api/holo/settings", []byte("{"))
		handler.UpdateSettings(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("UpdateSettings status=%d want=%d", rec.Code, http.StatusBadRequest)
		}

		ctx, rec = newAPITestContext(http.MethodPatch, "/api/holo/settings/llm", []byte("{"))
		handler.UpdateLLMSettings(ctx)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("UpdateLLMSettings status=%d want=%d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("get logs/settings and update success", func(t *testing.T) {
		applier := &stubSettingsApplier{}
		settingsSvc := settings.NewSettingsService(filepath.Join(t.TempDir(), "settings.json"), settings.Settings{
			AlarmAdvanceMinutes: 5,
			ScraperProxyEnabled: false,
		}, newDiscardLogger())

		handler := &SettingsAPIHandler{APIHandler: &APIHandler{
			logger:          newDiscardLogger(),
			activity:        newActivityLoggerForTest(t),
			settings:        settingsSvc,
			settingsApplier: applier,
		}}

		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/settings/logs", nil)
		handler.GetLogs(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("GetLogs status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		ctx, rec = newAPITestContext(http.MethodGet, "/api/holo/settings", nil)
		handler.GetSettings(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("GetSettings status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		ctx, rec = newAPITestContext(http.MethodPatch, "/api/holo/settings", []byte(`{"alarmAdvanceMinutes":7,"scraperProxyEnabled":true}`))
		handler.UpdateSettings(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("UpdateSettings status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		ctx, rec = newAPITestContext(http.MethodPatch, "/api/holo/settings/llm", []byte(`{"memberNewsWeeklyRunNow":true}`))
		handler.UpdateLLMSettings(ctx)
		if rec.Code != http.StatusOK {
			t.Fatalf("UpdateLLMSettings status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !applier.memberNewsApplied {
			t.Fatal("ApplyMemberNewsWeeklyRunNow should be called")
		}
	})
}
