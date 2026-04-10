package modules

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

func BuildSettingsService(targetMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) settings.ReadWriter {
	settingsPath := resolveSettingsFilePath()
	if logger != nil {
		logger.Info("Using settings file path", slog.String("path", settingsPath))
	}

	normalized := sharedchecker.NormalizeTargetMinutes(targetMinutes)
	defaultMinute := 5
	if len(normalized) > 0 && normalized[0] > 0 {
		defaultMinute = normalized[0]
	}

	return settings.NewSettingsService(settingsPath, settings.Settings{
		AlarmAdvanceMinutes: defaultMinute,
		ScraperProxyEnabled: scraperProxyEnabled,
	}, logger)
}

func ResolvePersistedTargetMinutes(targetMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) []int {
	settingsPath := resolveSettingsFilePath()
	if _, err := os.Stat(settingsPath); err != nil {
		return sharedchecker.NormalizeTargetMinutes(targetMinutes)
	}

	svc := BuildSettingsService(targetMinutes, scraperProxyEnabled, logger)
	current := svc.Get().AlarmAdvanceMinutes
	if current <= 0 {
		return sharedchecker.NormalizeTargetMinutes(targetMinutes)
	}

	return sharedchecker.BuildRuntimeTargetMinutes(current)
}

func resolveSettingsFilePath() string {
	dir := strings.TrimSpace(os.Getenv("SETTINGS_DIR"))
	if dir == "" {
		dir = "data"
	}
	return filepath.Join(dir, "settings.json")
}
