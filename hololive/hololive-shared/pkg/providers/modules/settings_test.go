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
	svc := BuildSettingsService([]int{9, 5, 1}, false, logger)
	current := svc.Get()
	current.AlarmAdvanceMinutes = 1
	svc.Update(current)

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

	got := ResolvePersistedTargetMinutes([]int{9, 5, 1}, false, nil)
	want := sharedchecker.NormalizeTargetMinutes([]int{9, 5, 1})

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
