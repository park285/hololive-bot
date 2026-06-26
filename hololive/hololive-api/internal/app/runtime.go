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

// Runtime hosts the bot ingress, admin API and LLM scheduler planes in one Go
// process while retaining explicit component lifecycle boundaries.
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

// Close releases process resources after all listeners and background loops have
// been drained by Run. Component cleanup is idempotent in the existing runtime
// implementations, so this also safely handles partial bootstrap failures.
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
