package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

func TestSettingsAPIHandlerAppliedThenResponseLostPreservesSavedSettings(t *testing.T) {
	t.Parallel()

	server, targetMinutes, puts := newSettingsResponseLossWorker(t)

	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	writer := &countingSettingsWriter{ReadWriter: mustNewTestSettingsService(t, settingsPath, settingssvc.Settings{AlarmAdvanceMinutes: 5})}
	handler := newSettingsHandler(Deps{
		Logger:   newDiscardLogger(),
		Activity: newActivityLoggerForTest(t),
		Settings: writer,
		Applier:  adminsettings.NewSettingsApplier(newTestAlarmClient(t, server.URL)),
	})

	firstCtx, firstRec := newAPITestContext(http.MethodPost, "/api/holo/settings", []byte(`{"alarmAdvanceMinutes":10}`))
	handler.UpdateSettings(firstCtx)

	firstRuntime := decodeUpdateSettingsRuntime(t, firstRec)
	assert.Equal(t, true, firstRuntime["alarm_applied"])
	assert.NotEmpty(t, firstRuntime["alarm_target_minutes"])

	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/settings", []byte(`{"alarmAdvanceMinutes":7}`))
	handler.UpdateSettings(ctx)

	runtime := decodeUpdateSettingsRuntime(t, rec)
	assert.Equal(t, false, runtime["alarm_applied"])
	assert.Contains(t, runtime["alarm_reason"], "outcome_unknown")
	assert.NotContains(t, runtime, "alarm_target_minutes")
	assert.NotContains(t, runtime, "alarm_outcome")
	assert.Equal(t, 7, writer.Get().AlarmAdvanceMinutes)
	assert.Equal(t, []int{7, 3, 1}, targetMinutes())
	assert.Equal(t, int32(2), writer.updates.Load())
	assert.Equal(t, int32(2), puts.Load())

	reloaded, err := adminsettings.NewSettingsService(settingsPath, settingssvc.Settings{AlarmAdvanceMinutes: 5})
	require.NoError(t, err)
	assert.Equal(t, 7, reloaded.Get().AlarmAdvanceMinutes)
	assert.Equal(t, []int{7, 3, 1}, reloaded.Get().TargetMinutes)

	getCtx, getRec := newAPITestContext(http.MethodGet, "/api/holo/settings", nil)
	handler.GetSettings(getCtx)

	getRuntime := decodeUpdateSettingsRuntime(t, getRec)
	assert.NotContains(t, getRuntime, "alarm_target_minutes")
}

func newSettingsResponseLossWorker(t *testing.T) (*httptest.Server, func() []int, *atomic.Int32) {
	t.Helper()

	worker, targetMinutes := newAlarmSettingsWorker(t, []int{5, 3, 1})

	var puts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if puts.Add(1) == 1 {
			worker.ServeHTTP(w, r)

			return
		}

		rec := httptest.NewRecorder()
		worker.ServeHTTP(rec, r)

		if rec.Code != http.StatusOK {
			t.Errorf("worker status = %d, want 200", rec.Code)
		}

		conn, _, hijackErr := http.NewResponseController(w).Hijack()
		if hijackErr != nil {
			t.Errorf("hijack response: %v", hijackErr)

			return
		}

		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close response connection: %v", closeErr)
		}
	}))
	t.Cleanup(server.Close)

	return server, targetMinutes, &puts
}
