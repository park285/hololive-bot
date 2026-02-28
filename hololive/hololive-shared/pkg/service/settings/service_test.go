package settings

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestSettingsService_LoadDefaultAndPersist(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "settings.json")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	defaults := Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: true,
	}
	svc := NewSettingsService(filePath, defaults, logger)
	got := svc.Get()
	if got.AlarmAdvanceMinutes != 5 {
		t.Fatalf("expected default 5, got %d", got.AlarmAdvanceMinutes)
	}
	if !got.ScraperProxyEnabled {
		t.Fatalf("expected default scraper proxy enabled true, got false")
	}

	updated := Settings{AlarmAdvanceMinutes: 12, ScraperProxyEnabled: false}
	if err := svc.Update(updated); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	reloaded := NewSettingsService(filePath, defaults, logger)
	got = reloaded.Get()
	if got.AlarmAdvanceMinutes != 12 {
		t.Fatalf("expected persisted 12, got %d", got.AlarmAdvanceMinutes)
	}
	if got.ScraperProxyEnabled {
		t.Fatalf("expected persisted scraper proxy enabled false, got true")
	}
}
