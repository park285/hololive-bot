package api

import (
	"bytes"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"

	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

// 파일 저장 실패는 기존 설정을 보존하고 worker 적용이나 성공 활동 기록 전에 요청을 끝낸다.
func TestSettingsAPIHandler_UpdateSettings_PersistenceFailurePreservesStateAndSkipsWorker(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}

	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	previous := settingssvc.Settings{AlarmAdvanceMinutes: 5, TargetMinutes: []int{5, 1}}
	service := mustNewTestSettingsService(t, path, previous, newDiscardLogger())

	if err := service.Update(previous); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	before, readErr := fs.ReadFile(os.DirFS(dir), "settings.json")
	if readErr != nil {
		t.Fatalf("read seeded settings: %v", readErr)
	}

	worker, puts := newAlarmWorkerStub(t, http.StatusOK)
	activity := &recordingSettingsActivityLogger{}
	handler := &SettingsHandler{
		Logger:          newDiscardLogger(),
		Settings:        service,
		Activity:        activity,
		SettingsApplier: sharedsettings.NewLocalSettingsApplier(alarm.NewClient(worker.URL, newDiscardLogger())),
	}

	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // G302: 쓰기 불가 디렉터리 재현용 모드
		t.Fatalf("make settings directory read-only: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: t.TempDir() 정리를 위한 모드 복구
			t.Errorf("restore settings directory mode: %v", err)
		}
	})

	ctx, rec := newSettingsTestContext(t, []byte(`{"alarmAdvanceMinutes":7}`))
	handler.UpdateSettings(ctx)

	assertErrorResponse(t, rec, http.StatusInternalServerError, "Failed to update settings")

	if got := puts.Load(); got != 0 {
		t.Fatalf("worker PUT count=%d, want 0 after persistence failure", got)
	}

	if activity.calls != 0 {
		t.Fatalf("settings activity count=%d, want 0 after persistence failure", activity.calls)
	}

	assertSettingsPersistenceFailureState(t, service, path, previous, before)
}

func assertSettingsPersistenceFailureState(t *testing.T, service *settingssvc.Service, path string, previous settingssvc.Settings, before []byte) {
	t.Helper()

	if got := service.Get(); got.AlarmAdvanceMinutes != previous.AlarmAdvanceMinutes || !slices.Equal(got.TargetMinutes, previous.TargetMinutes) {
		t.Fatalf("published settings after persistence failure=%+v, want %+v", got, previous)
	}

	after, readErr := fs.ReadFile(os.DirFS(filepath.Dir(path)), "settings.json")
	if readErr != nil {
		t.Fatalf("read settings after persistence failure: %v", readErr)
	}

	if !bytes.Equal(after, before) {
		t.Fatalf("settings file changed after persistence failure: before=%s after=%s", before, after)
	}

	stored, found, err := settingssvc.ReadFile(path)
	if err != nil || !found {
		t.Fatalf("read stored settings: found=%t err=%v", found, err)
	}

	if stored.AlarmAdvanceMinutes != previous.AlarmAdvanceMinutes || !slices.Equal(stored.TargetMinutes, previous.TargetMinutes) {
		t.Fatalf("stored settings after persistence failure=%+v, want %+v", stored, previous)
	}
}
