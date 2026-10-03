package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	adminruntime "github.com/kapu/hololive-api/internal/planes/admin/runtime"
	botruntime2 "github.com/kapu/hololive-api/internal/planes/bot/runtime"
	llmruntime "github.com/kapu/hololive-api/internal/planes/llm/runtime"
	youtuberuntime "github.com/kapu/hololive-api/internal/planes/youtube/runtime"
	"github.com/kapu/hololive-shared/pkg/applifecycle"
	"github.com/kapu/hololive-shared/pkg/constants"
)

// Runtime은 bot ingress, admin API, LLM scheduler, YouTube plane을 하나의 Go
// 프로세스에서 호스팅하되, 컴포넌트별 lifecycle 경계는 명시적으로 유지한다.
type Runtime struct {
	Config *apiconfig.RuntimeConfig
	Logger *slog.Logger

	Bot     *botruntime2.BotRuntime
	Admin   *adminruntime.AdminAPIRuntime
	LLM     *llmruntime.LLMSchedulerRuntime
	YouTube *youtuberuntime.Runtime

	group      runtimeGroup
	closeSteps []func(context.Context) error
	closeInit  sync.Once
	closeGate  chan struct{}
	closeDone  []bool
	closeMu    sync.Mutex
	closeErr   error
}

type runtimeGroup interface {
	Start(context.Context, chan<- error)
	Shutdown(context.Context) error
}

func BuildRuntime(ctx context.Context, appConfig *apiconfig.RuntimeConfig, logger *slog.Logger) (*Runtime, error) {
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
	admin   *adminruntime.AdminAPIRuntime
	llm     *llmruntime.LLMSchedulerRuntime
	youtube *youtuberuntime.Runtime
}

func buildAPIPlanes(ctx context.Context, appConfig *apiconfig.RuntimeConfig, logger *slog.Logger) (planes apiPlanes, retErr error) {
	// 성공한 plane은 즉시 같은 owner에 등록하고, 후속 생성 실패에서 모두 회수한다.
	defer func() {
		if retErr == nil {
			return
		}

		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.AppTimeout.Shutdown)

		defer cancel()

		retErr = errors.Join(retErr, planes.closeContext(cleanupCtx))
	}()

	var err error

	planes.llm, err = llmruntime.BuildLLMSchedulerRuntime(ctx, appConfig.LLM, logger.With(slog.String("plane", "llm")))
	if err != nil {
		return planes, fmt.Errorf("build llm plane: %w", err)
	}

	planes.admin, err = adminruntime.BuildAdminAPIRuntime(ctx, appConfig.Admin, logger.With(slog.String("plane", "admin")))
	if err != nil {
		return planes, fmt.Errorf("build admin plane: %w", err)
	}

	planes.bot, err = botruntime2.BuildRuntime(ctx, appConfig.Bot, logger.With(slog.String("plane", "bot")))
	if err != nil {
		return planes, fmt.Errorf("build bot plane: %w", err)
	}

	// 관리 plane이 바꾼 ACL을 같은 요청 안에서 봇 판정에 반영한다.
	planes.bot.ACL.Follow(planes.admin.ACL)

	youtubeResult := buildOptionalYouTubePlane(ctx, appConfig, logger)
	if youtubeResult.err != nil {
		return planes, fmt.Errorf("build youtube plane: %w", youtubeResult.err)
	}

	planes.youtube = youtubeResult.runtime
	if err := installAPIWorkerRegistry(ctx, appConfig, planes.bot, planes.youtube); err != nil {
		return planes, fmt.Errorf("build worker registry: %w", err)
	}

	return planes, nil
}

type optionalYouTubePlaneResult struct {
	runtime *youtuberuntime.Runtime
	err     error
}

func buildOptionalYouTubePlane(ctx context.Context, appConfig *apiconfig.RuntimeConfig, logger *slog.Logger) optionalYouTubePlaneResult {
	if !appConfig.YouTube.Enabled {
		return optionalYouTubePlaneResult{}
	}

	runtime, err := youtuberuntime.Build(ctx, &appConfig.YouTube, &appConfig.Bot.Postgres, logger.With(slog.String("plane", "youtube")))
	if err != nil {
		return optionalYouTubePlaneResult{err: fmt.Errorf("build youtube runtime: %w", err)}
	}

	return optionalYouTubePlaneResult{runtime: runtime}
}

func (p apiPlanes) closeContext(ctx context.Context) error {
	var closeErr error

	for _, closeStep := range apiPlaneCloseSteps(p) {
		closeErr = errors.Join(closeErr, closeStep(ctx))
	}

	return closeErr
}

func assembleAPIRuntime(appConfig *apiconfig.RuntimeConfig, logger *slog.Logger, planes apiPlanes) *Runtime {
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

func apiPlaneCloseSteps(planes apiPlanes) []func(context.Context) error {
	return []func(context.Context) error{
		planes.bot.CloseContext,
		planes.admin.CloseContext,
		planes.llm.CloseContext,
		planes.youtube.CloseContext,
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

// Close는 기동 rollback 등 context가 없는 호출에서도 제한된 시간으로 plane 자원을 회수한다.
func (r *Runtime) Close() {
	if r == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)

	defer cancel()

	if err := r.CloseContext(ctx); err != nil && r.Logger != nil {
		r.Logger.Error("aggregate runtime cleanup failed", slog.String("error", sharedlogging.RedactDiagnostic(err.Error())))
	}
}

// CloseContext는 모든 plane이 공유하는 남은 종료 예산을 전달하고 정리 오류를 보존한다.
// 각 plane은 자신의 작업 종료를 확인한 뒤 자원을 해제하며 두 번째 정리를 병렬로 시작하지 않는다.
func (r *Runtime) CloseContext(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.closeInit.Do(func() {
		r.closeGate = make(chan struct{}, 1)
		r.closeDone = make([]bool, len(r.closeSteps))
	})

	select {
	case r.closeGate <- struct{}{}:
	case <-ctx.Done():
		r.recordCloseError(fmt.Errorf("wait for aggregate resource cleanup: %w", ctx.Err()))

		return r.closeError()
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() { <-r.closeGate }()

		for i, closeStep := range r.closeSteps {
			if closeStep == nil || r.closeDone[i] {
				continue
			}

			err := closeStep(ctx)
			// 과거 drain 오류는 실제 정리 완료와 다르다. 실패한 step은 같은 plane owner에 다시 합류한다.
			r.closeDone[i] = err == nil
			r.recordCloseError(err)
		}
	}()

	select {
	case <-done:
		return r.closeError()
	case <-ctx.Done():
		r.recordCloseError(fmt.Errorf("join aggregate resource cleanup: %w", ctx.Err()))

		return r.closeError()
	}
}

func (r *Runtime) recordCloseError(err error) {
	r.closeMu.Lock()
	defer r.closeMu.Unlock()

	if err != nil {
		r.closeErr = errors.Join(r.closeErr, err)
	}
}

func (r *Runtime) closeError() error {
	r.closeMu.Lock()
	defer r.closeMu.Unlock()

	return r.closeErr
}
