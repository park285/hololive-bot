package modules

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

func BuildSettingsService(targetMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) settings.ReadWriter {
	settingsPath := resolveSettingsFilePath()
	if logger != nil {
		logger.Info("Using settings file path", slog.String("path", settingsPath))
	}

	policy := sharedchecker.NewTargetMinutePolicyFromConfigured(targetMinutes)

	return settings.NewSettingsService(settingsPath, settings.Settings{
		AlarmAdvanceMinutes: policy.PrimaryAdvanceMinute(),
		ScraperProxyEnabled: scraperProxyEnabled,
		TargetMinutes:       policy.Clone(),
	}, logger)
}

func ResolvePersistedTargetMinutes(targetMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) []int {
	_ = scraperProxyEnabled

	settingsPath := resolveSettingsFilePath()
	configuredPolicy := sharedchecker.NewTargetMinutePolicyFromConfigured(targetMinutes)
	resolvedConfigured := configuredPolicy.Clone()
	if _, err := os.Stat(settingsPath); err != nil {
		logResolvedTargetMinutes(logger, "config-missing", resolvedConfigured)
		return resolvedConfigured
	}

	persisted, err := readPersistedSettings(settingsPath)
	if err != nil {
		if logger != nil {
			logger.Warn("Failed to inspect persisted alarm advance minutes", slog.String("path", settingsPath), slog.String("error", err.Error()))
		}
		logResolvedTargetMinutes(logger, "invalid-persisted", resolvedConfigured)
		return resolvedConfigured
	}

	if hasPositiveTargetMinutes(persisted.TargetMinutes) {
		resolved := sharedchecker.NewTargetMinutePolicyFromPersisted(valueOrDefault(persisted.AlarmAdvanceMinutes), persisted.TargetMinutes).Clone()
		logResolvedTargetMinutes(logger, "persisted-settings", resolved)
		return resolved
	}

	if persisted.AlarmAdvanceMinutes == nil || *persisted.AlarmAdvanceMinutes <= 0 {
		logResolvedTargetMinutes(logger, "invalid-persisted", resolvedConfigured)
		return resolvedConfigured
	}

	resolved := sharedchecker.NewTargetMinutePolicyFromRuntimeAdvance(*persisted.AlarmAdvanceMinutes).Clone()
	logResolvedTargetMinutes(logger, "persisted-settings", resolved)
	return resolved
}

func resolveSettingsFilePath() string {
	dir := strings.TrimSpace(os.Getenv("SETTINGS_DIR"))
	if dir == "" {
		dir = "data"
	}
	return filepath.Join(dir, "settings.json")
}

type persistedSettings struct {
	AlarmAdvanceMinutes *int  `json:"alarmAdvanceMinutes"`
	TargetMinutes       []int `json:"targetMinutes"`
}

func readPersistedSettings(settingsPath string) (persistedSettings, error) {
	file, err := os.Open(settingsPath)
	if err != nil {
		return persistedSettings{}, fmt.Errorf("open settings file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	var persisted persistedSettings
	if err := json.NewDecoder(file).Decode(&persisted); err != nil {
		return persistedSettings{}, fmt.Errorf("decode settings file: %w", err)
	}

	return persisted, nil
}

func logResolvedTargetMinutes(logger *slog.Logger, source string, resolved []int) {
	if logger == nil {
		return
	}

	logger.Info("Resolved target minutes",
		slog.String("source", source),
		slog.Any("resolved_target_minutes", resolved),
	)
}

func hasPositiveTargetMinutes(targetMinutes []int) bool {
	for _, minute := range targetMinutes {
		if minute > 0 {
			return true
		}
	}

	return false
}

func valueOrDefault(v *int) int {
	if v == nil {
		return 0
	}

	return *v
}
