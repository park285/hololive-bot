package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/client/majorevent"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/client/membernews"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command"
)

func ResolveLLMSchedulerClients(
	appConfig *config.Config,
	logger *slog.Logger,
) (majorEventRepository command.MajorEventRepository, memberNewsService command.MemberNewsService) {
	if appConfig.LLMSchedulerURL == "" {
		logger.Warn("LLM scheduler URL not configured; majorevent/membernews commands disabled",
			slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"),
		)

		return nil, nil
	}

	return majorevent.New(appConfig.LLMSchedulerURL, appConfig.Server.APIKey),
		membernews.New(appConfig.LLMSchedulerURL, appConfig.Server.APIKey)
}
