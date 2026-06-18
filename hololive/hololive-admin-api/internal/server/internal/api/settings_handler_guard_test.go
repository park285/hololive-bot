package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

type recordingSettingsActivityLogger struct {
	calls int
}

func (r *recordingSettingsActivityLogger) Log(string, string, map[string]any) {
	r.calls++
}

func newGuardTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/holo/settings", http.NoBody)
	return ctx, rec
}

func TestSettingsHandler_Guards(t *testing.T) {
	gin.SetMode(gin.TestMode)

	settingsService := settingssvc.NewSettingsService(
		filepath.Join(t.TempDir(), "settings.json"),
		settingssvc.Settings{},
		newDiscardLogger(),
	)

	cases := []struct {
		name        string
		guard       func(h *SettingsHandler, c *gin.Context) bool
		nilHandler  *SettingsHandler
		okHandler   *SettingsHandler
		wantMessage string
	}{
		{
			name:        "requireAlarm",
			guard:       func(h *SettingsHandler, c *gin.Context) bool { return h.requireAlarm(c) },
			nilHandler:  &SettingsHandler{},
			okHandler:   &SettingsHandler{Alarm: &stubAlarmCRUDForServer{}},
			wantMessage: "alarm service not available",
		},
		{
			name:        "requireSettings",
			guard:       func(h *SettingsHandler, c *gin.Context) bool { return h.requireSettings(c) },
			nilHandler:  &SettingsHandler{},
			okHandler:   &SettingsHandler{Settings: settingsService},
			wantMessage: "settings service not available",
		},
		{
			name:        "requireApplier",
			guard:       func(h *SettingsHandler, c *gin.Context) bool { return h.requireApplier(c) },
			nilHandler:  &SettingsHandler{},
			okHandler:   &SettingsHandler{SettingsApplier: testSettingsApplier{}},
			wantMessage: "settings applier not available",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"_nil_503", func(t *testing.T) {
			ctx, rec := newGuardTestContext()
			if tc.guard(tc.nilHandler, ctx) {
				t.Fatalf("%s: guard passed with nil dependency, want fail", tc.name)
			}
			assertErrorResponse(t, rec, http.StatusServiceUnavailable, tc.wantMessage)
		})

		t.Run(tc.name+"_present_pass", func(t *testing.T) {
			ctx, rec := newGuardTestContext()
			if !tc.guard(tc.okHandler, ctx) {
				t.Fatalf("%s: guard failed with dependency present, want pass", tc.name)
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: guard wrote status=%d on pass, want untouched %d", tc.name, rec.Code, http.StatusOK)
			}
			if rec.Body.Len() != 0 {
				t.Fatalf("%s: guard wrote body %q on pass, want empty", tc.name, rec.Body.String())
			}
		})
	}
}

func TestSettingsHandler_SafeLogger(t *testing.T) {
	custom := newDiscardLogger()
	if got := (&SettingsHandler{Logger: custom}).safeLogger(); got != custom {
		t.Fatalf("safeLogger returned configured logger mismatch")
	}
	if got := (&SettingsHandler{}).safeLogger(); got != slog.Default() {
		t.Fatalf("safeLogger with nil Logger did not fall back to slog.Default()")
	}
}

func TestSettingsHandler_LogActivity(t *testing.T) {
	rec := &recordingSettingsActivityLogger{}
	(&SettingsHandler{Activity: rec}).logActivity("t", "s", map[string]any{"k": "v"})
	if rec.calls != 1 {
		t.Fatalf("logActivity calls=%d want=1", rec.calls)
	}

	(&SettingsHandler{}).logActivity("t", "s", nil)
}
