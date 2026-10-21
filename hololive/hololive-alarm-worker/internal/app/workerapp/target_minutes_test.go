package workerapp

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/settings"
)

func TestResolvePersistedTargetMinutes_UsesConfiguredWhenSettingsMissing(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	got, err := resolvePersistedTargetMinutes(settingsPath, []int{5}, nil)
	if err != nil {
		t.Fatalf("resolvePersistedTargetMinutes() error = %v", err)
	}

	if want := []int{5, 3, 1}; !slices.Equal(got, want) {
		t.Fatalf("resolvePersistedTargetMinutes() = %v, want %v", got, want)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("settings dir stat error = %v", err)
	}
}

func TestResolvePersistedTargetMinutes_PreservesExplicitMultiTargetWhenSettingsMissing(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")

	got, err := resolvePersistedTargetMinutes(settingsPath, []int{30, 15, 5, 1}, nil)
	if err != nil {
		t.Fatalf("resolvePersistedTargetMinutes() error = %v", err)
	}

	if want := []int{30, 15, 5, 1}; !slices.Equal(got, want) {
		t.Fatalf("resolvePersistedTargetMinutes() = %v, want %v", got, want)
	}
}

// targetMinutes 없이 alarmAdvanceMinutes만 있던 구형 형식을 runtime 기본 목록으로 해석하던 분기와, 잘못된 파일을
// 설정값으로 대신하던 경로를 지웠다. 저장 파일이 있는데 읽을 수 없으면 기동 오류로 드러나야 한다.
func TestResolvePersistedTargetMinutes_RejectsUnsupportedPersistedSettings(t *testing.T) {
	for name, content := range map[string]string{
		"legacy advance-only format": `{"alarmAdvanceMinutes":1}`,
		"invalid advance minute":     `{"alarmAdvanceMinutes":0}`,
		"empty target minutes":       `{"alarmAdvanceMinutes":5,"targetMinutes":[]}`,
		"undecodable":                `{"alarmAdvanceMinutes":`,
	} {
		t.Run(name, func(t *testing.T) {
			settingsPath := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(settingsPath, []byte(content), 0o600); err != nil {
				t.Fatalf("write settings file: %v", err)
			}

			got, err := resolvePersistedTargetMinutes(settingsPath, []int{30, 15, 5, 1}, slog.New(slog.DiscardHandler))
			if err == nil {
				t.Fatalf("resolvePersistedTargetMinutes() = %v, nil; want error", got)
			}

			if !strings.Contains(err.Error(), "resolve persisted target minutes") {
				t.Fatalf("resolvePersistedTargetMinutes() error = %v", err)
			}
		})
	}
}

func TestResolvePersistedTargetMinutes_PreservesExplicitTargetsAcrossUnrelatedUpdate(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")

	logger := slog.New(slog.DiscardHandler)

	// API admin이 저장하는 형식 그대로 기록한 뒤 무관한 필드만 갱신한다.
	service, err := settings.NewSettingsService(settingsPath, settings.Settings{AlarmAdvanceMinutes: 30, TargetMinutes: []int{30, 15, 5, 1}}, logger)
	if err != nil {
		t.Fatalf("NewSettingsService() error = %v", err)
	}

	current := service.Get()

	current.AlarmAdvanceMinutes = 15

	if err = service.Update(current); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	got, err := resolvePersistedTargetMinutes(settingsPath, []int{30, 15, 5, 1}, logger)
	if err != nil {
		t.Fatalf("resolvePersistedTargetMinutes() error = %v", err)
	}

	if want := []int{30, 15, 5, 1}; !slices.Equal(got, want) {
		t.Fatalf("resolvePersistedTargetMinutes() = %v, want %v", got, want)
	}
}

func TestResolvePersistedTargetMinutes_PreservesExplicitStoredTargetMinutes(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.json")

	logger := slog.New(slog.DiscardHandler)

	if err := os.WriteFile(settingsPath, []byte(`{"alarmAdvanceMinutes":5,"targetMinutes":[5,1]}`), 0o600); err != nil {
		t.Fatalf("write settings file: %v", err)
	}

	got, err := resolvePersistedTargetMinutes(settingsPath, []int{9, 5, 1}, logger)
	if err != nil {
		t.Fatalf("resolvePersistedTargetMinutes() error = %v", err)
	}

	if want := []int{5, 1}; !slices.Equal(got, want) {
		t.Fatalf("resolvePersistedTargetMinutes() = %v, want %v", got, want)
	}
}
