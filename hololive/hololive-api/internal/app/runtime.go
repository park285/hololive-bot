package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	adminruntime "github.com/kapu/hololive-api/internal/planes/admin/runtime"
	botruntime "github.com/kapu/hololive-api/internal/planes/bot/runtime"
	llmruntime "github.com/kapu/hololive-api/internal/planes/llm/runtime"
	"github.com/kapu/hololive-shared/pkg/applifecycle"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedlifecycle "github.com/park285/shared-go/pkg/runtime/lifecycle"
)

// Runtime은 bot ingress, admin API, LLM scheduler plane을 하나의 Go 프로세스에서
// 호스팅하되, 컴포넌트별 lifecycle 경계는 명시적으로 유지한다.
type Runtime struct {
	Config *config.HololiveAPIConfig
	Logger *slog.Logger

	Bot   *botruntime.Runtime
	Admin *adminruntime.Runtime
	LLM   *llmruntime.Runtime

	group *applifecycle.GroupRuntime
}

func BuildRuntime(ctx context.Context, appConfig *config.HololiveAPIConfig, logger *slog.Logger) (*Runtime, error) {
	if appConfig == nil {
		return nil, errors.New("hololive-api config must not be nil")
	}
	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}

	llm, err := llmruntime.Build(ctx, appConfig.LLM, logger.With(slog.String("plane", "llm")))
	if err != nil {
		return nil, fmt.Errorf("build llm plane: %w", err)
	}

	admin, err := adminruntime.Build(ctx, appConfig.Admin, logger.With(slog.String("plane", "admin")))
	if err != nil {
		llm.Close()
		return nil, fmt.Errorf("build admin plane: %w", err)
	}

	bot, err := botruntime.Build(ctx, appConfig.Bot, logger.With(slog.String("plane", "bot")))
	if err != nil {
		admin.Close()
		llm.Close()
		return nil, fmt.Errorf("build bot plane: %w", err)
	}

	runtime := &Runtime{
		Config: appConfig,
		Logger: logger,
		Bot:    bot,
		Admin:  admin,
		LLM:    llm,
	}
	runtime.group = applifecycle.NewGroupRuntime(logger,
		applifecycle.GroupComponent{
			Name:     "llm",
			Start:    llm.Start,
			Shutdown: llm.Shutdown,
		},
		applifecycle.GroupComponent{
			Name:  "admin",
			Start: admin.Start,
			Shutdown: func(ctx context.Context) error {
				admin.Shutdown(ctx)
				return nil
			},
		},
		applifecycle.GroupComponent{
			Name:  "bot",
			Start: bot.Start,
			Shutdown: func(ctx context.Context) error {
				bot.Shutdown(ctx)
				return nil
			},
		},
	)
	return runtime, nil
}

func (r *Runtime) Run() {
	if r == nil || r.group == nil {
		return
	}

	err := sharedlifecycle.Run(sharedlifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start:           r.group.Start,
		OnSignal: func(signal os.Signal) {
			r.Logger.Info("hololive-api shutdown signal received", slog.String("signal", signal.String()))
		},
		OnError: func(err error) {
			r.Logger.Error("hololive-api runtime error", slog.Any("error", err))
		},
		BeforeShutdown: func() {
			r.Logger.Info("hololive-api draining runtime planes")
		},
		Shutdown: r.group.Shutdown,
	})
	if err != nil {
		r.Logger.Error("hololive-api shutdown completed with errors", slog.Any("error", err))
	}
}

// Close는 Run이 모든 listener와 background loop을 drain한 뒤 프로세스 자원을 해제한다.
// 컴포넌트 cleanup은 멱등(idempotent)이라 부분 bootstrap 실패 상태에서 호출돼도 안전하다.
func (r *Runtime) Close() {
	if r == nil {
		return
	}
	if r.Bot != nil {
		r.Bot.Close()
	}
	if r.Admin != nil {
		r.Admin.Close()
	}
	if r.LLM != nil {
		r.LLM.Close()
	}
}
