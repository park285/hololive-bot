package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-api/internal/service/activity"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

type boundarySettingsApplier struct {
	testSettingsApplier

	calls int
}

func (a *boundarySettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) (sharedsettings.AlarmAdvanceMinutesApplyResult, error) {
	a.calls++
	return a.testSettingsApplier.ApplyAlarmAdvanceMinutes(ctx, minutes)
}

func TestSettingsRejectUnknownFieldsBeforePersistenceOrApply(t *testing.T) {
	store := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{AlarmAdvanceMinutes: 5}, newDiscardLogger())
	applier := &boundarySettingsApplier{}
	h := &SettingsHandler{Logger: newDiscardLogger(), Settings: store, SettingsApplier: applier}

	for _, body := range []string{
		`{"scraperProxyEnabled":true}`,
		`{"alarmAdvanceMinute":10}`,
		`{"alarmAdvanceMinutes":10,"scraperProxyEnabled":true}`,
	} {
		ctx, recorder := newSettingsTestContext(t, []byte(body))
		h.UpdateSettings(ctx)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unknown setting status=%d body=%s", recorder.Code, recorder.Body.String())
		}

		if store.Get().AlarmAdvanceMinutes != 5 || applier.calls != 0 {
			t.Fatal("rejected setting changed persisted or applied state")
		}
	}
}

func TestRoomNameAbsentFromProductionAndActivityLogs(t *testing.T) {
	// stdout를 사용하는 실제 운영 sanitizer를 거치므로 이 검사는 병렬 실행하지 않는다.
	const canary = "PRIVATE_ROOM_NAME_CANARY"

	output, err := os.CreateTemp(t.TempDir(), "stdout-")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = output.Close() })

	prior := os.Stdout

	os.Stdout = output

	t.Cleanup(func() { os.Stdout = prior })

	logger := sharedlogging.NewLogger()
	audit := activity.NewActivityLogger(logger)
	h := &SettingsHandler{Logger: logger, Activity: audit, Alarm: &roomNameRecordingAlarm{}}
	ctx, recorder := newSettingsTestContext(t, []byte(`{"roomId":"private-room-id","roomName":"`+canary+`"}`))
	h.SetRoomName(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}

	bytes, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}

	assertRoomNameLogIsPrivate(t, bytes, canary)
}

func assertRoomNameLogIsPrivate(t *testing.T, raw []byte, canary string) {
	t.Helper()

	if strings.Contains(string(raw), canary) || strings.Contains(string(raw), "private-room-id") {
		t.Fatalf("private room data leaked: %s", raw)
	}

	if !strings.Contains(string(raw), "name_update") {
		t.Fatalf("room name activity missing: %s", raw)
	}
}
