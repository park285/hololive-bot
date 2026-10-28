package adminruntime

import (
	"fmt"
	"log/slog"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

// adminAPISettings는 admin plane의 settings 저장소와 적용기를 묶는다. 저장소는 settings.json을 기동 때 한 번 읽으며
// 읽을 수 없는 파일은 기동 실패다(settings.ReadFile).
type adminAPISettings struct {
	service settingssvc.ReadWriter
	applier sharedsettings.SettingsApplier
}

func buildAdminAPISettings(
	appConfig *apiconfig.AdminPlaneConfig,
	alarmMode *alarmModeComponents,
	logger *slog.Logger,
) (adminAPISettings, error) {
	service, err := sharedsettings.BuildSettingsService(appConfig.SettingsFilePath, appConfig.Notification.AdvanceMinutes, logger)
	if err != nil {
		return adminAPISettings{}, fmt.Errorf("settings service: %w", err)
	}

	return adminAPISettings{service: service, applier: sharedsettings.NewLocalSettingsApplier(alarmMode.AlarmClient)}, nil
}
