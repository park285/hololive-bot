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

func ResolveAlarmAdvanceMinutes(advanceMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) []int {
	settingsPath := resolveSettingsFilePath()

	if _, err := os.Stat(settingsPath); err != nil {
		if os.IsNotExist(err) {
			return advanceMinutes
		}
		if logger != nil {
			logger.Warn("Failed to stat settings file, using config alarm advance minutes",
				slog.String("path", settingsPath),
				slog.Any("error", err),
			)
		}
		return advanceMinutes
	}

	defaults := settings.Settings{
		AlarmAdvanceMinutes: defaultAlarmAdvanceMinute(advanceMinutes),
		ScraperProxyEnabled: scraperProxyEnabled,
	}

	svc := settings.NewSettingsService(settingsPath, defaults, logger)
	alarmAdvanceMinutes := svc.Get().AlarmAdvanceMinutes
	if alarmAdvanceMinutes <= 0 {
		return advanceMinutes
	}

	if logger != nil {
		logger.Info("Applying persisted alarm advance minutes",
			slog.Int("alarm_advance_minutes", alarmAdvanceMinutes),
			slog.String("settings_path", settingsPath),
		)
	}

	return []int{alarmAdvanceMinutes, 3, 1}
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
