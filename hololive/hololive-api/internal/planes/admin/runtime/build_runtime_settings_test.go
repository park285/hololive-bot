package adminruntime

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// settings 파일 정본은 admin plane이 소유하므로 읽을 수 없는 파일은 admin plane 기동에서 실패해야 한다.
func TestBuildAdminAPISettingsRejectsMalformedSettingsFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"alarmAdvanceMinutes":`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := buildAdminAPISettings(&apiconfig.AdminPlaneConfig{
		SettingsFilePath: path,
		Notification:     settings.NotificationConfig{AdvanceMinutes: []int{10, 5, 1}},
	}, &alarmModeComponents{}, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("malformed settings file must fail admin plane startup")
	}
}
