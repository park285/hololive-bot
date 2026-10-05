package adminruntime

import (
	"fmt"
	"log/slog"
	"strings"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	triggerclient "github.com/kapu/hololive-api/internal/planes/admin/internal/client/trigger"
	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/system"
	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

// adminAPISettings는 admin plane의 settings 저장소와 적용기를 묶는다. 저장소는 settings.json을 기동 때 한 번 읽으며
// 읽을 수 없는 파일은 기동 실패다(settings.ReadFile).
type adminAPISettings struct {
	service       settingssvc.ReadWriter
	applier       sharedsettings.SettingsApplier
	triggerClient *triggerclient.Client
}

func buildAdminAPISettings(
	appConfig *apiconfig.AdminPlaneConfig,
	alarmMode *alarmModeComponents,
	logger *slog.Logger,
) (adminAPISettings, error) {
	service, err := sharedmodules.BuildSettingsService(appConfig.SettingsFilePath, appConfig.Notification.AdvanceMinutes, logger)
	if err != nil {
		return adminAPISettings{}, fmt.Errorf("settings service: %w", err)
	}

	applier, triggerClient, err := buildAdminAPISettingsApplier(appConfig, alarmMode, logger)
	if err != nil {
		return adminAPISettings{}, fmt.Errorf("settings applier: %w", err)
	}

	return adminAPISettings{service: service, applier: applier, triggerClient: triggerClient}, nil
}

func buildAdminAPISettingsApplier(
	appConfig *apiconfig.AdminPlaneConfig,
	alarmMode *alarmModeComponents,
	logger *slog.Logger,
) (sharedsettings.SettingsApplier, *triggerclient.Client, error) {
	localSettingsApplier := sharedsettings.NewLocalSettingsApplier(alarmMode.AlarmClient)
	settingsApplier := newBotSettingsApplier(localSettingsApplier, nil, logger)

	if strings.TrimSpace(appConfig.LLMSchedulerURL) == "" {
		logger.Warn("LLM scheduler URL not configured; trigger routes and membernews run-now are disabled", slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"))

		return settingsApplier, nil, nil //nolint:nilnil // scheduler URL 미설정은 trigger client를 끄는 계약값이며 오류가 아니다.
	}

	majorEventTriggerClient, err := triggerclient.NewClient(appConfig.LLMSchedulerURL, appConfig.Server.APIKey, logger, appConfig.InternalH3)
	if err != nil {
		return nil, nil, fmt.Errorf("build llm scheduler trigger client: %w", err)
	}

	return newBotSettingsApplier(localSettingsApplier, majorEventTriggerClient, logger), majorEventTriggerClient, nil
}

func buildAdminAPISystemCollector(appConfig *apiconfig.AdminPlaneConfig) *system.Collector {
	return system.NewCollector([]system.ServiceEndpoint{
		{Name: "llm-scheduler", URL: appConfig.Services.LLMSchedulerHealthURL},
		{Name: "twentyq", URL: appConfig.Services.GameBotTwentyQHealthURL},
		{Name: "turtlesoup", URL: appConfig.Services.GameBotTurtleHealthURL},
	}, appConfig.InternalH3, system.WithServiceName("hololive-admin-api"))
}
