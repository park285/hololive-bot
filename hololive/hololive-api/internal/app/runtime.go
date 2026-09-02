package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-api/internal/planes/admin/app"
	botruntime2 "github.com/kapu/hololive-api/internal/planes/bot/runtime"
	llmruntime "github.com/kapu/hololive-api/internal/planes/llm/runtime"
	youtuberuntime "github.com/kapu/hololive-api/internal/planes/youtube/runtime"
	"github.com/kapu/hololive-shared/pkg/applifecycle"
	"github.com/kapu/hololive-shared/pkg/config/settings/apiplane"
)

// Runtime은 bot ingress, admin API, LLM scheduler, YouTube plane을 하나의 Go
// 프로세스에서 호스팅하되, 컴포넌트별 lifecycle 경계는 명시적으로 유지한다.
type Runtime struct {
	Config *apiplane.RuntimeConfig
	Logger *slog.Logger

	Bot     *botruntime2.BotRuntime
	Admin   *app.AdminAPIRuntime
	LLM     *llmruntime.LLMSchedulerRuntime
	YouTube *youtuberuntime.Runtime

	group      runtimeGroup
	closeOnce  sync.Once
	closeSteps []func()
}

type runtimeGroup interface {
	Start(context.Context, chan<- error)
	Shutdown(context.Context) error
}

func BuildRuntime(ctx context.Context, appConfig *apiplane.RuntimeConfig, logger *slog.Logger) (*Runtime, error) {
	if appConfig == nil {
		return nil, errors.New("hololive-api config must not be nil")
	}

	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}

	planes, err := buildAPIPlanes(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("build API planes: %w", err)
	}

	return assembleAPIRuntime(appConfig, logger, planes), nil
}

type apiPlanes struct {
	bot     *botruntime2.BotRuntime
	admin   *app.AdminAPIRuntime
	llm     *llmruntime.LLMSchedulerRuntime
	youtube *youtuberuntime.Runtime
}

func buildAPIPlanes(ctx context.Context, appConfig *apiplane.RuntimeConfig, logger *slog.Logger) (apiPlanes, error) {
	llm, err := llmruntime.BuildLLMSchedulerRuntime(ctx, appConfig.LLM, logger.With(slog.String("plane", "llm")))
	if err != nil {
		return apiPlanes{}, fmt.Errorf("build llm plane: %w", err)
	}

	admin, err := app.BuildAdminAPIRuntime(ctx, appConfig.Admin, logger.With(slog.String("plane", "admin")))
	if err != nil {
		llm.Close()

		return apiPlanes{}, fmt.Errorf("build admin plane: %w", err)
	}

	bot, err := botruntime2.BuildRuntime(ctx, appConfig.Bot, logger.With(slog.String("plane", "bot")))
	if err != nil {
		admin.Close()
		llm.Close()

		return apiPlanes{}, fmt.Errorf("build bot plane: %w", err)
	}

	planes := apiPlanes{bot: bot, admin: admin, llm: llm}
	youtubeResult := buildOptionalYouTubePlane(ctx, appConfig, logger)

	if youtubeResult.err != nil {
		planes.shutdown()

		return apiPlanes{}, fmt.Errorf("build youtube plane: %w", youtubeResult.err)
	}

	planes.youtube = youtubeResult.runtime

	if err := installAPIWorkerRegistry(ctx, appConfig, bot, planes.youtube); err != nil {
		planes.shutdown()

		return apiPlanes{}, fmt.Errorf("build worker registry: %w", err)
	}

	return planes, nil
}

type optionalYouTubePlaneResult struct {
	runtime *youtuberuntime.Runtime
	err     error
}

func buildOptionalYouTubePlane(ctx context.Context, appConfig *apiplane.RuntimeConfig, logger *slog.Logger) optionalYouTubePlaneResult {
	if !appConfig.YouTube.Enabled {
		return optionalYouTubePlaneResult{}
	}

	runtime, err := youtuberuntime.Build(ctx, &appConfig.YouTube, &appConfig.Bot.Postgres, logger.With(slog.String("plane", "youtube")))
	if err != nil {
		return optionalYouTubePlaneResult{err: fmt.Errorf("build youtube runtime: %w", err)}
	}

	return optionalYouTubePlaneResult{runtime: runtime}
}

func (p apiPlanes) shutdown() {
	if p.youtube != nil {
		p.youtube.Close()
	}

	if p.bot != nil {
		p.bot.Close()
	}

	if p.admin != nil {
		p.admin.Close()
	}

	if p.llm != nil {
		p.llm.Close()
	}
}

func assembleAPIRuntime(appConfig *apiplane.RuntimeConfig, logger *slog.Logger, planes apiPlanes) *Runtime {
	runtime := &Runtime{
		Config:  appConfig,
		Logger:  logger,
		Bot:     planes.bot,
		Admin:   planes.admin,
		LLM:     planes.llm,
		YouTube: planes.youtube,
	}

	runtime.group = applifecycle.NewGroupRuntime(logger, apiPlaneComponents(planes)...)
	runtime.closeSteps = apiPlaneCloseSteps(planes)

	return runtime
}

func apiPlaneComponents(planes apiPlanes) []applifecycle.GroupComponent {
	components := []applifecycle.GroupComponent{
		{Name: "llm", Start: planes.llm.Start, Shutdown: planes.llm.Shutdown},
		{Name: "admin", Start: planes.admin.Start, Shutdown: planes.admin.Shutdown},
		{Name: "bot", Start: planes.bot.Start, Shutdown: planes.bot.Shutdown},
	}
	if planes.youtube == nil {
		return components
	}

	return append([]applifecycle.GroupComponent{{
		Name:     "youtube",
		Start:    planes.youtube.Start,
		Shutdown: planes.youtube.Shutdown,
	}}, components...)
}

func apiPlaneCloseSteps(planes apiPlanes) []func() {
	return []func(){
		func() {
			if planes.bot != nil {
				planes.bot.Close()
			}
		},
		func() {
			if planes.admin != nil {
				planes.admin.Close()
			}
		},
		func() {
			if planes.llm != nil {
				planes.llm.Close()
			}
		},
		func() {
			if planes.youtube != nil {
				planes.youtube.Close()
			}
		},
	}
}

func (r *Runtime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil || r.group == nil {
		return
	}

	r.group.Start(ctx, errCh)
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || r.group == nil {
		return nil
	}

	if err := r.group.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown runtime planes: %w", err)
	}

	return nil
}

// Close는 lifecycle coordinator가 모든 listener와 background loop을 drain한 뒤 프로세스 자원을 해제한다.
// 컴포넌트 cleanup은 멱등(idempotent)이라 부분 bootstrap 실패 상태에서 호출돼도 안전하다.
func (r *Runtime) Close() {
	if r == nil {
		return
	}

	r.closeOnce.Do(func() {
		for _, closeStep := range r.closeSteps {
			if closeStep != nil {
				closeStep()
			}
		}
	})
}
