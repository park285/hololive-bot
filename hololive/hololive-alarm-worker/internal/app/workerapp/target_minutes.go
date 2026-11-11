package workerapp

import (
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

func resolvePersistedTargetMinutes(settingsPath string, targetMinutes []int, logger *slog.Logger) ([]int, error) {
	stored, found, err := settings.ReadFile(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("resolve persisted target minutes: %w", err)
	}

	if !found {
		resolvedConfigured := targetpolicy.NewTargetMinutePolicyFromConfigured(targetMinutes).Clone()
		logResolvedTargetMinutes(logger, "config-missing", resolvedConfigured)

		return resolvedConfigured, nil
	}

	logResolvedTargetMinutes(logger, "persisted-settings", stored.TargetMinutes)

	return stored.TargetMinutes, nil
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
