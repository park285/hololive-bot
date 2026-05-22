package bootstrap

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"

	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/majoreventclient"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/membernewsclient"
)

func ResolveLLMSchedulerClients(
	appConfig *config.Config,
	logger *slog.Logger,
) (command.MajorEventRepository, command.MemberNewsService) {
	if appConfig.LLMSchedulerURL == "" {
		logger.Warn("LLM scheduler URL not configured; majorevent/membernews commands disabled",
			slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"),
		)

		return nil, nil
	}

	return majoreventclient.New(appConfig.LLMSchedulerURL, appConfig.Server.APIKey),
		membernewsclient.New(appConfig.LLMSchedulerURL, appConfig.Server.APIKey)
}
