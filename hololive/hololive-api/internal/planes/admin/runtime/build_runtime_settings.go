package adminruntime

import (
	"fmt"
	"log/slog"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
)

type adminAPISettings struct {
	service adminsettings.ReadWriter
	applier adminsettings.SettingsApplier
}

func buildAdminAPISettings(
	appConfig *apiconfig.AdminPlaneConfig,
	alarmClient alarmProvider,
	logger *slog.Logger,
) (adminAPISettings, error) {
	service, err := adminsettings.BuildSettingsService(appConfig.SettingsFilePath, appConfig.Notification.AdvanceMinutes, logger)
	if err != nil {
		return adminAPISettings{}, fmt.Errorf("settings service: %w", err)
	}

	return adminAPISettings{service: service, applier: adminsettings.NewSettingsApplier(alarmClient)}, nil
}
