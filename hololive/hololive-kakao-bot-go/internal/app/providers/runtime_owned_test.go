package providers

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAlarmAdvanceMinutes_UsesPersistedSettings(t *testing.T) {
	settingsDir := t.TempDir()
	t.Setenv("SETTINGS_DIR", settingsDir)

	payload, err := json.Marshal(map[string]any{
		"alarmAdvanceMinutes": 9,
		"scraperProxyEnabled": true,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), payload, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got := ResolveAlarmAdvanceMinutes([]int{15, 3, 1}, false, slog.New(slog.DiscardHandler))
	want := []int{9, 3, 1}

	if len(got) != len(want) {
		t.Fatalf("len(ResolveAlarmAdvanceMinutes()) = %d, want %d", len(got), len(want))
	}

	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("ResolveAlarmAdvanceMinutes()[%d] = %d, want %d", idx, got[idx], want[idx])
		}
	}
}

func TestProvideSettingsService_UsesMaxAdvanceMinuteAsDefault(t *testing.T) {
	settingsDir := t.TempDir()
	t.Setenv("SETTINGS_DIR", settingsDir)

	service := ProvideSettingsService([]int{1, 7, 3}, false, slog.New(slog.DiscardHandler))
	if service == nil {
		t.Fatal("ProvideSettingsService() returned nil")
	}

	got := service.Get()
	if got.AlarmAdvanceMinutes != 7 {
		t.Fatalf("AlarmAdvanceMinutes = %d, want %d", got.AlarmAdvanceMinutes, 7)
	}
	if got.ScraperProxyEnabled {
		t.Fatal("ScraperProxyEnabled = true, want false")
	}
}
