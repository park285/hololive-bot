package bootstrap

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/client/majorevent"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/client/membernews"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// LLMSchedulerClients는 major event·member news client와 그 H3 transport 소유권이다.
// Scheduler URL이 없으면 모든 필드가 비고 명령은 꺼진다.
type LLMSchedulerClients struct {
	MajorEvent handlercore.MajorEventRepository
	MemberNews handlercore.MemberNewsService
	// Transports는 bot plane Close에서 닫을 client다.
	Transports []io.Closer
}

// ResolveLLMSchedulerClients는 LLM_SCHEDULER_INTERNAL_URL이 설정된 경우에만 major event·member news client를 만든다.
// URL이 설정됐는데 client를 만들지 못하면(HOLOLIVE_INTERNAL_H3_* 누락 포함) 명령을 조용히 끄지 않고 오류를 돌려
// bot plane 기동을 실패시킨다(stack audit 2026-09-26).
func ResolveLLMSchedulerClients(
	appConfig *settings.Config,
	logger *slog.Logger,
) (LLMSchedulerClients, error) {
	if appConfig.LLMSchedulerURL == "" {
		logger.Warn("LLM scheduler URL not configured; majorevent/membernews commands disabled",
			slog.String("env", "LLM_SCHEDULER_INTERNAL_URL"),
		)

		return LLMSchedulerClients{}, nil
	}

	majorEventClient, err := majorevent.New(appConfig.LLMSchedulerURL, appConfig.Server.APIKey, appConfig.InternalH3)
	if err != nil {
		return LLMSchedulerClients{}, fmt.Errorf("major event client: %w", err)
	}

	memberNewsClient, err := membernews.New(appConfig.LLMSchedulerURL, appConfig.Server.APIKey, appConfig.InternalH3)
	if err != nil {
		if closeErr := majorEventClient.Close(); closeErr != nil {
			return LLMSchedulerClients{}, errors.Join(fmt.Errorf("member news client: %w", err), fmt.Errorf("rollback major event client: %w", closeErr))
		}

		return LLMSchedulerClients{}, fmt.Errorf("member news client: %w", err)
	}

	return LLMSchedulerClients{
		MajorEvent: majorEventClient,
		MemberNews: memberNewsClient,
		Transports: []io.Closer{majorEventClient, memberNewsClient},
	}, nil
}
