package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/llm/notification"
	llmruntime "github.com/kapu/hololive-api/internal/planes/llm/runtime"
	"github.com/kapu/hololive-shared/pkg/constants"
)

// RerunNotification은 운영 CLI의 정기 알림 수동 재실행이다. 실행 중인 hololive-api와 같은 설정·자원 조립으로
// llm plane scheduler만 만들어 한 번 호출하고, HTTP 서버와 정기 schedule은 시작하지 않는다.
func RerunNotification(ctx context.Context, appConfig *apiconfig.RuntimeConfig, kind string, logger *slog.Logger) (_ notification.Result, retErr error) {
	if appConfig == nil {
		return notification.Result{}, errors.New("hololive-api config must not be nil")
	}

	if logger == nil {
		return notification.Result{}, errors.New("logger must not be nil")
	}

	infra, err := buildAPIInfrastructure(ctx, appConfig, logger)
	if err != nil {
		return notification.Result{}, fmt.Errorf("build process infrastructure: %w", err)
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.AppTimeout.Shutdown)
		defer cancel()

		retErr = errors.Join(retErr, infra.CloseContext(cleanupCtx))
	}()

	llm, err := llmruntime.BuildLLMSchedulerRuntime(ctx, appConfig.LLM, infra.llm, logger.With(slog.String("plane", "llm")))
	if err != nil {
		return notification.Result{}, fmt.Errorf("build llm plane: %w", err)
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.AppTimeout.Shutdown)
		defer cancel()

		retErr = errors.Join(retErr, llm.CloseContext(cleanupCtx))
	}()

	result, err := llm.RerunNotification(ctx, kind)
	if err != nil {
		return result, fmt.Errorf("rerun %s: %w", kind, err)
	}

	return result, nil
}
