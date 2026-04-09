package providers

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/kapu/hololive-shared/pkg/service/settings"
)

func ProvideSettingsService(advanceMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) settings.ReadWriter {
	settingsPath := resolveSettingsFilePath()
	if logger != nil {
		logger.Info("Using settings file path", slog.String("path", settingsPath))
	}

	return settings.NewSettingsService(settingsPath, settings.Settings{
		AlarmAdvanceMinutes: defaultAlarmAdvanceMinute(advanceMinutes),
		ScraperProxyEnabled: scraperProxyEnabled,
	}, logger)
}

func defaultAlarmAdvanceMinute(advanceMinutes []int) int {
	maxMinute := 0
	for _, minute := range advanceMinutes {
		if minute > maxMinute {
			maxMinute = minute
		}
	}

	if maxMinute <= 0 {
		return 5
	}

	return maxMinute
}

func resolveSettingsFilePath() string {
	dir := strings.TrimSpace(os.Getenv("SETTINGS_DIR"))
	if dir == "" {
		dir = "data"
	}
	return filepath.Join(dir, "settings.json")
}
