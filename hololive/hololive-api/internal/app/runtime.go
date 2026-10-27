package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	adminruntime "github.com/kapu/hololive-api/internal/planes/admin/runtime"
	botruntime2 "github.com/kapu/hololive-api/internal/planes/bot/runtime"
	llmruntime "github.com/kapu/hololive-api/internal/planes/llm/runtime"
	youtuberuntime "github.com/kapu/hololive-api/internal/planes/youtube/runtime"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/applifecycle"
	"github.com/kapu/hololive-shared/pkg/constants"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

// processStopTimeout은 drain과 자원 회수를 합친 process stop 상한이다. Compose의 45초 grace 안에서
// 뒤이은 telemetry flush(5초)와 logger close까지 끝나도록 30초로 둔다.
const processStopTimeout = 30 * time.Second

// Runtime은 bot ingress, admin API, LLM scheduler, YouTube plane을 하나의 Go
// 프로세스에서 호스팅하되, 컴포넌트별 lifecycle 경계는 명시적으로 유지한다.
type Runtime struct {
	Config *apiconfig.RuntimeConfig
	Logger *slog.Logger

	Bot     *botruntime2.BotRuntime
	Admin   *adminruntime.AdminAPIRuntime
	LLM     *llmruntime.LLMSchedulerRuntime
	YouTube *youtuberuntime.Runtime

	group           runtimeGroup
	closeSteps      []func(context.Context) error
	closeInit       sync.Once
	closeDone       chan struct{}
	closeMu         sync.Mutex
	closeErr        error
	stopOwnsCleanup atomic.Bool
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

	// plane 사이 호출은 같은 프로세스의 인스턴스를 직접 주입한다. bot은 llm의 구독·digest 서비스를, admin은
	// bot이 소유한 Iris client를 빌린다. 빌린 자원은 모든 plane drain이 끝난 뒤 소유 plane의 CloseContext가 닫으므로
	// GroupRuntime의 plane 정지 순서에 의존하지 않는다.
	planes.bot, err = botruntime2.BuildRuntime(ctx, appConfig.Bot, planes.infra.bot, botruntime2.Dependencies{
		ACL:        planes.infra.acl,
		Holodex:    planes.infra.holodex,
		MajorEvent: planes.llm.MajorEventSubscriptions,
		MemberNews: planes.llm.MemberNews,
	}, logger.With(slog.String("plane", "bot")))
	if err != nil {
		return planes, fmt.Errorf("build bot plane: %w", err)
	}

	planes.admin, err = adminruntime.BuildAdminAPIRuntime(ctx, appConfig.Admin, planes.infra.admin, adminruntime.Dependencies{
		ACL:       planes.infra.acl,
		Holodex:   planes.infra.holodex,
		IrisRooms: planes.bot.IrisRooms,
	}, logger.With(slog.String("plane", "admin")))
	if err != nil {
		return planes, fmt.Errorf("build admin plane: %w", err)
	}

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

// Run은 production 경로의 유일한 process signal owner다. 종료 원인 요약은 반환값으로 전달하고,
// bootstrap이 반환 오류를 ERROR로 기록한 뒤 exit 1로 바꾼다.
func (r *Runtime) Run() error {
	if err := lifecycle.Run(context.Background(), r.lifecycleOptions()); err != nil {
		return fmt.Errorf("run aggregate runtime: %w", err)
	}

	return nil
}

func (r *Runtime) lifecycleOptions() lifecycle.Options {
	return lifecycle.Options{
		ShutdownTimeout: processStopTimeout,
		Start:           r.Start,
		OnSignal: func(sig os.Signal) {
			r.Logger.Info("Received shutdown signal",
				slog.String("runtime", "hololive-api"),
				slog.String("signal", sig.String()),
			)
		},
		// 종료 처리가 끝나기 전에 원인을 남겨야 drain이 시한까지 막혀도 fatal 원인을 잃지 않는다.
		OnError: func(err error) {
			r.Logger.Error("hololive-api runtime error", slog.String("error", sharedlogging.RedactDiagnostic(err.Error())))
		},
		BeforeShutdown: func() {
			r.Logger.Info("hololive-api draining runtime planes")
		},
		Shutdown: r.shutdownAndClose,
	}
}

// lifecycle.Run은 Shutdown hook을 동기로 기다리므로 ctx를 따르지 않는 plane이 있으면 시한을 넘긴다.
// 시한에서 hook을 끊어 processStopTimeout을 강제하고, 남은 drain·회수는 프로세스 종료와 함께 버린다.
func (r *Runtime) shutdownAndClose(ctx context.Context) error {
	r.stopOwnsCleanup.Store(true)

	done := make(chan error, 1)

	go func() { done <- r.drainAndClose(ctx) }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// 시한까지 쌓인 회수 오류는 버려지는 goroutine에만 남고 Close도 다시 보고하지 않으므로 함께 반환한다.
		return errors.Join(fmt.Errorf("stop aggregate runtime: %w", ctx.Err()), r.closeError())
	}
}

// plane drain은 AppTimeout.Shutdown 안에서 끝내고, 자원 회수는 processStopTimeout의 남은 예산을 쓴다.
func (r *Runtime) drainAndClose(ctx context.Context) error {
	drainCtx, cancel := context.WithTimeout(ctx, constants.AppTimeout.Shutdown)
	shutdownErr := r.Shutdown(drainCtx)

	cancel()

	// stop 시한 뒤에 끝난 drain은 프로세스 종료와 겹치므로 자원 회수를 새로 시작하지 않는다.
	if ctx.Err() != nil {
		return shutdownErr
	}

	return errors.Join(shutdownErr, r.CloseContext(ctx))
}

// Close는 기동 rollback 등 context가 없는 호출에서도 제한된 시간으로 plane 자원을 회수한다.
func (r *Runtime) Close() {
	if r == nil {
		return
	}

	// Run의 종료 hook이 process stop 예산 안에서 회수와 오류 보고를 이미 소유했다. 다시 합류하면
	// Compose grace를 넘길 수 있고 같은 오류를 두 번 기록한다.
	if r.stopOwnsCleanup.Load() {
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

// apiInfrastructure는 bot pool을 멤버 정본과 ACL의 backing pool로 소유한다.
// Plane에 전달한 모듈에는 종료 권한이 없고 모든 plane join 후 한 번만 정리한다.
type apiInfrastructure struct {
	bot, admin, llm *sharedmodules.InfraModule
	// acl은 process 하나에 하나뿐인 ACL Service다. admin 변경과 bot 판정이 같은 메모리 snapshot을 쓴다.
	acl *acl.Service
	// holodex는 bot(!예정·!일정)과 admin(/streams/*)이 빌려 쓰는 process 하나의 Holodex·공식 일정 서비스다.
	// 두 plane이 같은 Valkey cache를 쓰면서 retry scheduler·circuit breaker·cache-fill 조정을 따로 두면 장애 때
	// 원천을 따로 다시 조회하고 breaker 상태도 나뉘므로 하나만 만든다.
	holodex        *apifoundation.ScraperHolodexFoundation
	closeResources func()
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

	bot, err := sharedmodules.BuildInfraModule(ctx, sharedmodules.InfraOptions{Valkey: config.Bot.Valkey, Postgres: config.Bot.Postgres, Pool: databaseproviders.RuntimePool{Name: "bot", ReservedConns: 1}}, logger)
	if err != nil {
		return nil, fmt.Errorf("build bot infrastructure: %w", err)
	}

	var (
		cleanupAdmin, cleanupLLM func()
		holodex                  *apifoundation.ScraperHolodexFoundation
	)

	cleanup := sync.OnceFunc(func() {
		// 공유 Holodex 서비스와 캐시를 먼저 join한 뒤 모든 pool과 Valkey를 닫는다.
		if holodex != nil {
			holodex.HolodexService.Stop()
		}

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

	admin, cleanupAdmin, err := databaseproviders.ProvideDatabaseResources(ctx, &config.Admin.Postgres, databaseproviders.RuntimePool{Name: "admin"}, logger)
	if err != nil {
		return nil, fmt.Errorf("provide admin database resources: %w", err)
	}

	llm, cleanupLLM, err := databaseproviders.ProvideDatabaseResources(ctx, &config.LLM.Postgres, databaseproviders.RuntimePool{Name: "llm_scheduler"}, logger)
	if err != nil {
		return nil, fmt.Errorf("provide llm database resources: %w", err)
	}

	aclMode, err := acl.ParseACLModeStrict(config.Bot.Kakao.ACLMode)
	if err != nil {
		return nil, fmt.Errorf("invalid KAKAO_ACL_MODE: %w", err)
	}

	aclService, err := acl.NewACLService(ctx, bot.Postgres, logger, config.Bot.Kakao.ACLEnabled, aclMode, config.Bot.Kakao.Rooms)
	if err != nil {
		return nil, fmt.Errorf("build ACL service: %w", err)
	}

	holodex, err = apifoundation.BuildScraperHolodex(ctx, apifoundation.ScraperHolodexOptions{
		Holodex:          config.Bot.Holodex,
		OfficialSchedule: config.Bot.OfficialSchedule,
	}, bot.MemberCache, bot.Cache, logger)
	if err != nil {
		return nil, fmt.Errorf("build holodex foundation: %w", err)
	}

	return &apiInfrastructure{
		acl:            aclService,
		holodex:        holodex,
		bot:            &sharedmodules.InfraModule{Cache: bot.Cache, Postgres: bot.Postgres, MemberRepository: bot.MemberRepository, MemberCache: bot.MemberCache},
		admin:          &sharedmodules.InfraModule{Cache: bot.Cache, Postgres: admin.Service, MemberRepository: member.NewMemberRepository(admin.Service, logger), MemberCache: bot.MemberCache},
		llm:            &sharedmodules.InfraModule{Cache: bot.Cache, Postgres: llm.Service, MemberRepository: member.NewMemberRepository(llm.Service, logger), MemberCache: bot.MemberCache},
		closeResources: cleanup,
	}, nil
}
