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
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/member"
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
	closeDone  chan struct{}
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
	infra   *apiInfrastructure
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

	planes.infra, err = buildAPIInfrastructure(ctx, appConfig, logger)
	if err != nil {
		return planes, fmt.Errorf("build process infrastructure: %w", err)
	}

	planes.llm, err = llmruntime.BuildLLMSchedulerRuntime(ctx, appConfig.LLM, planes.infra.llm, logger.With(slog.String("plane", "llm")))
	if err != nil {
		return planes, fmt.Errorf("build llm plane: %w", err)
	}

	planes.admin, err = adminruntime.BuildAdminAPIRuntime(ctx, appConfig.Admin, planes.infra.admin, logger.With(slog.String("plane", "admin")))
	if err != nil {
		return planes, fmt.Errorf("build admin plane: %w", err)
	}

	planes.bot, err = botruntime2.BuildRuntime(ctx, appConfig.Bot, planes.infra.bot, logger.With(slog.String("plane", "bot")))
	if err != nil {
		return planes, fmt.Errorf("build bot plane: %w", err)
	}

	// 관리 plane이 바꾼 ACL을 같은 요청 안에서 봇 판정에 반영한다.
	planes.bot.ACL.Follow(planes.admin.ACL)

	// 비활성 executor도 영속 큐 관측과 profile 진단의 수명을 소유한다.
	planes.youtube, err = youtuberuntime.Build(ctx, &appConfig.YouTube, &appConfig.Bot.Postgres, logger.With(slog.String("plane", "youtube")))
	if err != nil {
		return planes, fmt.Errorf("build youtube plane: %w", err)
	}

	if err := installAPIWorkerRegistry(ctx, appConfig, planes.bot, planes.youtube); err != nil {
		return planes, fmt.Errorf("build worker registry: %w", err)
	}

	return planes, nil
}

func (p apiPlanes) closeContext(ctx context.Context) error {
	owner := &Runtime{closeSteps: apiPlaneCloseSteps(p)}
	return owner.CloseContext(ctx)
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
		func(ctx context.Context) error {
			if !planes.bot.ResourceUsersJoined() || !planes.admin.ResourceUsersJoined() || !planes.llm.ResourceUsersJoined() {
				return errors.New("shared infrastructure retained: plane resource users have not joined")
			}

			return planes.infra.CloseContext(ctx)
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

// CloseContext는 호출자 대기 예산과 자원 소유자의 join 수명을 분리한다.
// 한 번 시작한 정리는 모든 borrower가 끝난 뒤 backing pool과 Valkey를 닫는다.
func (r *Runtime) CloseContext(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.closeInit.Do(func() {
		r.closeDone = make(chan struct{})

		go func() {
			defer close(r.closeDone)

			joinCtx := context.WithoutCancel(ctx)

			for _, closeStep := range r.closeSteps {
				if closeStep != nil {
					r.recordCloseError(closeStep(joinCtx))
				}
			}
		}()
	})

	select {
	case <-r.closeDone:
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

// apiInfrastructure는 bot pool을 멤버 정본의 backing pool로 소유한다.
// Plane에 전달한 모듈에는 종료 권한이 없고 모든 plane join 후 한 번만 정리한다.
type apiInfrastructure struct {
	bot, admin, llm *sharedmodules.InfraModule
	closeResources  func()
}

func (r *apiInfrastructure) CloseContext(context.Context) error {
	if r != nil && r.closeResources != nil {
		r.closeResources()
	}

	return nil
}

func buildAPIInfrastructure(ctx context.Context, config *apiconfig.RuntimeConfig, logger *slog.Logger) (_ *apiInfrastructure, retErr error) {
	if config == nil || config.Bot == nil || config.Admin == nil || config.LLM == nil {
		return nil, errors.New("bot, admin and llm configs must not be nil")
	}

	bot, err := sharedmodules.BuildInfraModule(ctx, sharedmodules.InfraOptions{Valkey: config.Bot.Valkey, Postgres: config.Bot.Postgres, PoolName: "bot"}, logger)
	if err != nil {
		return nil, fmt.Errorf("build bot infrastructure: %w", err)
	}

	var cleanupAdmin, cleanupLLM func()

	cleanup := sync.OnceFunc(func() {
		// 공유 캐시를 먼저 join한 뒤 모든 pool과 Valkey를 닫는다.
		bot.StopMemberCache()

		if cleanupLLM != nil {
			cleanupLLM()
		}

		if cleanupAdmin != nil {
			cleanupAdmin()
		}

		bot.Cleanup()
	})

	defer func() {
		if retErr != nil {
			cleanup()
		}
	}()

	admin, cleanupAdmin, err := databaseproviders.ProvideDatabaseResources(ctx, &config.Admin.Postgres, "admin", logger)
	if err != nil {
		return nil, fmt.Errorf("provide admin database resources: %w", err)
	}

	llm, cleanupLLM, err := databaseproviders.ProvideDatabaseResources(ctx, &config.LLM.Postgres, "llm_scheduler", logger)
	if err != nil {
		return nil, fmt.Errorf("provide llm database resources: %w", err)
	}

	return &apiInfrastructure{
		bot:            &sharedmodules.InfraModule{Cache: bot.Cache, Postgres: bot.Postgres, MemberRepository: bot.MemberRepository, MemberCache: bot.MemberCache},
		admin:          &sharedmodules.InfraModule{Cache: bot.Cache, Postgres: admin.Service, MemberRepository: member.NewMemberRepository(admin.Service, logger), MemberCache: bot.MemberCache},
		llm:            &sharedmodules.InfraModule{Cache: bot.Cache, Postgres: llm.Service, MemberRepository: member.NewMemberRepository(llm.Service, logger), MemberCache: bot.MemberCache},
		closeResources: cleanup,
	}, nil
}
