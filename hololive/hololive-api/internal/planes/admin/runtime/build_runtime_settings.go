package adminruntime

import (
	"fmt"
	"log/slog"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	adminsettings "github.com/kapu/hololive-api/internal/planes/admin/internal/service/settings"
)

// 저장소는 settings.json을 기동 때 한 번 읽으며 읽을 수 없는 파일은 기동 실패다(settings.ReadFile).
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
