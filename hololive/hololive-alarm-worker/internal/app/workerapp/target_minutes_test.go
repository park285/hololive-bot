package workerapp

import (
	jsonv2 "encoding/json/v2"
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

	// API admin writer는 settings.Settings를 그대로 기록한다. alarmAdvanceMinutes만 바뀐 파일도 저장된 target 목록을 쓴다.
	raw, err := jsonv2.Marshal(settings.Settings{AlarmAdvanceMinutes: 15, TargetMinutes: []int{30, 15, 5, 1}})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	if err = os.WriteFile(settingsPath, raw, 0o600); err != nil {
		t.Fatalf("write settings file: %v", err)
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
