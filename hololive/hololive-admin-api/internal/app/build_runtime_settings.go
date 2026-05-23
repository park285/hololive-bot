package app

import (
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"

	triggerclient "github.com/kapu/hololive-admin-api/internal/service/trigger"
	"github.com/kapu/hololive-admin-api/internal/service/system"
)

func buildAdminAPISettingsApplier(
	appConfig *config.Config,
	foundation *scraperHolodexProfileFoundation,
	alarmMode *alarmModeComponents,
	ytStack *providers.YouTubeStack,
	logger *slog.Logger,
) (sharedsettings.SettingsApplier, *triggerclient.Client) {
	localSettingsApplier := sharedsettings.NewLocalSettingsApplier(
		ytStack.GetService(),
		foundation.HolodexService,
		nil,
		alarmMode.AlarmCRUD,
	)
	settingsApplier := newBotSettingsApplier(localSettingsApplier, nil, logger)

	if strings.TrimSpace(appConfig.LLMSchedulerURL) == "" {
		logger.Warn("LLM scheduler URL not configured; trigger routes and membernews run-now are disabled", slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"))
		return settingsApplier, nil
	}

	majorEventTriggerClient := triggerclient.NewClient(appConfig.LLMSchedulerURL, appConfig.Server.APIKey, logger)
	return newBotSettingsApplier(localSettingsApplier, majorEventTriggerClient, logger), majorEventTriggerClient
}

func buildAdminAPISystemCollector(appConfig *config.Config) *system.Collector {
	return system.NewCollector([]system.ServiceEndpoint{
		{Name: "llm-scheduler", URL: appConfig.Services.LLMSchedulerHealthURL},
		{Name: "twentyq", URL: appConfig.Services.GameBotTwentyQHealthURL},
		{Name: "turtlesoup", URL: appConfig.Services.GameBotTurtleHealthURL},
	}, system.WithServiceName("hololive-admin-api"))
}
