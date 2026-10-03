package api

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

// countingSettingsWriter는 settings.json 기록 횟수를 센다.
type countingSettingsWriter struct {
	settingssvc.ReadWriter

	updates atomic.Int32
}

func (w *countingSettingsWriter) Update(next settingssvc.Settings) error {
	w.updates.Add(1)

	if err := w.ReadWriter.Update(next); err != nil {
		return fmt.Errorf("update settings: %w", err)
	}

	return nil
}

// newAlarmWorkerStub은 alarm-worker의 advance minutes PUT을 흉내 내고 받은 요청 수를 센다.
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

func newSettingsUpdateAPIHandler(t *testing.T, workerURL string) (*SettingsAPIHandler, *countingSettingsWriter) {
	t.Helper()

	writer := &countingSettingsWriter{
		ReadWriter: mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{
			AlarmAdvanceMinutes: 5,
		}, newDiscardLogger()),
	}

	handler := &SettingsAPIHandler{Handler: &Handler{
		logger:          newDiscardLogger(),
		activity:        newActivityLoggerForTest(t),
		settings:        writer,
		settingsApplier: sharedsettings.NewLocalSettingsApplier(alarm.NewClient(workerURL, newDiscardLogger())),
	}}

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

// alarm_advance 변경의 적용 경로는 admin UpdateSettings 하나다: settings.json 1회 기록 + worker PUT 1회.
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

// worker 적용 실패는 응답에 드러나야 한다. 다른 적용 경로(Pub/Sub 재적용)가 없으므로 성공으로 보고하면
// 운영자가 worker가 옛 값으로 도는 걸 알 수 없다.
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

// 0분은 저장이나 worker 적용 전에 거절하여 기존 양수 설정과 런타임을 보존한다.
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
