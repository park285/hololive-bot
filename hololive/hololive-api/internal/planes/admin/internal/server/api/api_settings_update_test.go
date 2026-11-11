package api

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

type countingSettingsWriter struct {
	adminsettings.ReadWriter

	updates atomic.Int32
}

func (w *countingSettingsWriter) Update(next settingssvc.Settings) error {
	w.updates.Add(1)

	if err := w.ReadWriter.Update(next); err != nil {
		return fmt.Errorf("update settings: %w", err)
	}

	return nil
}

func newAlarmWorkerStub(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var puts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != contractsalarm.SettingsPath {
			http.NotFound(w, r)

			return
		}

		puts.Add(1)

		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("drain worker request body: %v", err)
		}

		body := `{"success":false,"error":"worker down"}`

		if status == http.StatusOK {
			body = `{"success":true,"data":{"target_minutes":[7,3,1]}}`
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)

		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write worker response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return server, &puts
}

func newSettingsUpdateAPIHandler(t *testing.T, workerURL string) (*SettingsHandler, *countingSettingsWriter) {
	t.Helper()

	writer := &countingSettingsWriter{
		ReadWriter: mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{
			AlarmAdvanceMinutes: 5,
		}),
	}

	handler := newSettingsHandler(Deps{
		Logger:   newDiscardLogger(),
		Activity: newActivityLoggerForTest(t),
		Settings: writer,
		Applier:  adminsettings.NewSettingsApplier(newTestAlarmClient(t, workerURL)),
	})

	return handler, writer
}

func decodeUpdateSettingsRuntime(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("UpdateSettings status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any

	if err := jsonv2.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	runtime, ok := payload["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime payload missing: %#v", payload["runtime"])
	}

	return runtime
}

func TestSettingsAPIHandler_UpdateSettings_AppliesAlarmAdvanceOnceAndWritesOnce(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	worker, puts := newAlarmWorkerStub(t, http.StatusOK)
	handler, writer := newSettingsUpdateAPIHandler(t, worker.URL)

	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/settings", []byte(`{"alarmAdvanceMinutes":7}`))
	handler.UpdateSettings(ctx)

	runtime := decodeUpdateSettingsRuntime(t, rec)

	if got := puts.Load(); got != 1 {
		t.Fatalf("worker PUT count=%d want=1", got)
	}

	if got := writer.updates.Load(); got != 1 {
		t.Fatalf("settings.json write count=%d want=1", got)
	}

	if got := writer.Get().AlarmAdvanceMinutes; got != 7 {
		t.Fatalf("persisted AlarmAdvanceMinutes=%d want=7", got)
	}

	if got := runtime["alarm_applied"]; got != true {
		t.Fatalf("alarm_applied=%v want=true (runtime=%#v)", got, runtime)
	}
}

// 다른 적용 경로(Pub/Sub 재적용)가 없으므로 성공으로 보고하면 운영자가 worker가 옛 값으로 도는 걸 알 수 없다.
func TestSettingsAPIHandler_UpdateSettings_ReportsWorkerApplyFailure(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	worker, puts := newAlarmWorkerStub(t, http.StatusInternalServerError)
	handler, _ := newSettingsUpdateAPIHandler(t, worker.URL)

	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/settings", []byte(`{"alarmAdvanceMinutes":7}`))
	handler.UpdateSettings(ctx)

	runtime := decodeUpdateSettingsRuntime(t, rec)

	if got := puts.Load(); got != 1 {
		t.Fatalf("worker PUT count=%d want=1 (no retry)", got)
	}

	if got := runtime["alarm_applied"]; got != false {
		t.Fatalf("alarm_applied=%v want=false (runtime=%#v)", got, runtime)
	}

	if got, ok := runtime["alarm_reason"].(string); !ok || got == "" {
		t.Fatalf("alarm_reason missing on worker failure: %#v", runtime)
	}
}

func TestSettingsAPIHandler_UpdateSettings_RejectsZeroBeforeStoreAndWorker(t *testing.T) {
	t.Parallel()

	worker, targetMinutes, puts := newSettingsResponseLossWorker(t)
	handler, writer := newSettingsUpdateAPIHandler(t, worker.URL)
	previous := writer.Get()
	req, rec := newAPITestContext(http.MethodPost, "/api/holo/settings", []byte(`{"alarmAdvanceMinutes":0}`))
	handler.UpdateSettings(req)

	assertErrorResponse(t, rec, http.StatusBadRequest, "alarmAdvanceMinutes must be between 1 and 1440")

	if got := writer.updates.Load(); got != 0 {
		t.Fatalf("settings write count = %d, want 0", got)
	}

	if got := puts.Load(); got != 0 {
		t.Fatalf("worker PUT count = %d, want 0", got)
	}

	if got := writer.Get(); got.AlarmAdvanceMinutes != previous.AlarmAdvanceMinutes {
		t.Fatalf("settings advance = %d, want preserved %d", got.AlarmAdvanceMinutes, previous.AlarmAdvanceMinutes)
	}

	if got := targetMinutes(); len(got) != 3 || got[0] != 5 {
		t.Fatalf("worker targets = %v, want preserved [5 3 1]", got)
	}
}

// settings.update 응답과 settings.json 형식을 고정한다. 응답 settings와 파일은 shared settings.Settings가, runtime 키는
// admin settings 결과 타입이 정하므로 두 패키지 중 하나만 바뀌어도 iris-console·alarm-worker 계약이 깨진다.
func TestSettingsAPIHandler_UpdateSettings_PreservesWireAndStoredFileShape(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	worker, _ := newAlarmWorkerStub(t, http.StatusOK)
	dir := t.TempDir()
	handler := newSettingsHandler(Deps{
		Logger:   newDiscardLogger(),
		Activity: newActivityLoggerForTest(t),
		Settings: mustNewTestSettingsService(t, filepath.Join(dir, "settings.json"), settingssvc.Settings{AlarmAdvanceMinutes: 5}),
		Applier:  adminsettings.NewSettingsApplier(newTestAlarmClient(t, worker.URL)),
	})

	ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/settings", []byte(`{"alarmAdvanceMinutes":7}`))
	handler.UpdateSettings(ctx)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"status": "ok",
		"message": "Settings updated",
		"settings": {"alarmAdvanceMinutes": 7, "targetMinutes": [7, 3, 1]},
		"runtime": {"alarm_requested_advance_minutes": 7, "alarm_applied": true, "alarm_target_minutes": [7, 3, 1]}
	}`, rec.Body.String())

	stored, err := fs.ReadFile(os.DirFS(dir), "settings.json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"alarmAdvanceMinutes": 7, "targetMinutes": [7, 3, 1]}`, string(stored))

	getCtx, getRec := newAPITestContext(http.MethodGet, "/api/holo/settings", nil)
	handler.GetSettings(getCtx)

	require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())
	assert.JSONEq(t, `{
		"status": "ok",
		"settings": {"alarmAdvanceMinutes": 7, "targetMinutes": [7, 3, 1]},
		"runtime": {"alarm_target_minutes": [7, 3, 1]}
	}`, getRec.Body.String())
}
