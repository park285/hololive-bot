package modules

import (
	"log/slog"
	"os"
	"testing"

	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
)

func TestResolvePersistedTargetMinutes_UsesRuntimeNormalizationForStoredMinute(t *testing.T) {
	t.Setenv("SETTINGS_DIR", t.TempDir())

	logger := slog.New(slog.DiscardHandler)
	if err := os.WriteFile(resolveSettingsFilePath(), []byte(`{"alarmAdvanceMinutes":1,"scraperProxyEnabled":false}`), 0o600); err != nil {
		t.Fatalf("write legacy settings file: %v", err)
	}

	got := ResolvePersistedTargetMinutes([]int{9, 5, 1}, false, logger)
	want := sharedchecker.BuildRuntimeTargetMinutes(1)

	if len(got) != len(want) {
		t.Fatalf("ResolvePersistedTargetMinutes() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ResolvePersistedTargetMinutes() = %v, want %v", got, want)
		}
	}
}

func TestResolvePersistedTargetMinutes_FallsBackWhenSettingsMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SETTINGS_DIR", dir)

	got := ResolvePersistedTargetMinutes([]int{5}, false, nil)
	want := []int{5, 3, 1}

	if len(got) != len(want) {
		t.Fatalf("ResolvePersistedTargetMinutes() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ResolvePersistedTargetMinutes() = %v, want %v", got, want)
		}
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("settings dir stat error = %v", err)
	}
}

func TestResolvePersistedTargetMinutes_PreservesExplicitMultiTargetWhenSettingsMissing(t *testing.T) {
	t.Setenv("SETTINGS_DIR", t.TempDir())

	got := ResolvePersistedTargetMinutes([]int{30, 15, 5, 1}, false, nil)
	want := []int{30, 15, 5, 1}

	if len(got) != len(want) {
		t.Fatalf("ResolvePersistedTargetMinutes() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ResolvePersistedTargetMinutes() = %v, want %v", got, want)
		}
	}
}

func TestResolvePersistedTargetMinutes_FallsBackWhenPersistedMinuteIsInvalid(t *testing.T) {
	t.Setenv("SETTINGS_DIR", t.TempDir())

	logger := slog.New(slog.DiscardHandler)
	if err := os.WriteFile(resolveSettingsFilePath(), []byte(`{"alarmAdvanceMinutes":0,"scraperProxyEnabled":false}`), 0o600); err != nil {
		t.Fatalf("write invalid settings file: %v", err)
	}

	got := ResolvePersistedTargetMinutes([]int{30, 15, 5, 1}, false, logger)
	want := []int{30, 15, 5, 1}

	if len(got) != len(want) {
		t.Fatalf("ResolvePersistedTargetMinutes() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ResolvePersistedTargetMinutes() = %v, want %v", got, want)
		}
	}
}

func TestResolvePersistedTargetMinutes_PreservesExplicitTargetsAcrossUnrelatedUpdate(t *testing.T) {
	t.Setenv("SETTINGS_DIR", t.TempDir())

	logger := slog.New(slog.DiscardHandler)
	svc := BuildSettingsService([]int{30, 15, 5, 1}, false, logger)
	current := svc.Get()
	current.ScraperProxyEnabled = true
	if err := svc.Update(current); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	got := ResolvePersistedTargetMinutes([]int{30, 15, 5, 1}, false, logger)
	want := []int{30, 15, 5, 1}

	if len(got) != len(want) {
		t.Fatalf("ResolvePersistedTargetMinutes() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ResolvePersistedTargetMinutes() = %v, want %v", got, want)
		}
	}
}

func TestResolvePersistedTargetMinutes_HealsLegacyStoredTargetMinutes(t *testing.T) {
	t.Setenv("SETTINGS_DIR", t.TempDir())

	logger := slog.New(slog.DiscardHandler)
	if err := os.WriteFile(resolveSettingsFilePath(), []byte(`{"alarmAdvanceMinutes":5,"scraperProxyEnabled":false,"targetMinutes":[5,1]}`), 0o600); err != nil {
		t.Fatalf("write legacy settings file: %v", err)
	}

	got := ResolvePersistedTargetMinutes([]int{9, 5, 1}, false, logger)
	want := []int{5, 3, 1}

	if len(got) != len(want) {
		t.Fatalf("ResolvePersistedTargetMinutes() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ResolvePersistedTargetMinutes() = %v, want %v", got, want)
		}
	}
}
