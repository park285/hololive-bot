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

	apiconfig "github.com/kapu/hololive-api/internal/config"
	adminruntime "github.com/kapu/hololive-api/internal/planes/admin/runtime"
	botruntime2 "github.com/kapu/hololive-api/internal/planes/bot/runtime"
	youtuberuntime "github.com/kapu/hololive-api/internal/planes/youtube/runtime"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/constants"
	databaseproviders "github.com/kapu/hololive-shared/pkg/providers/database"
	holodexproviders "github.com/kapu/hololive-shared/pkg/providers/holodex"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

const processStopTimeout = 30 * time.Second

type Runtime struct {
	Config *apiconfig.RuntimeConfig
	Logger *slog.Logger

	Bot     *botruntime2.BotRuntime
	Admin   *adminruntime.AdminAPIRuntime
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
	youtube *youtuberuntime.Runtime
	infra   *apiInfrastructure
}

func buildAPIPlanes(ctx context.Context, appConfig *apiconfig.RuntimeConfig, logger *slog.Logger) (planes apiPlanes, retErr error) {
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

	planes.bot, err = botruntime2.BuildRuntime(ctx, appConfig.Bot, planes.infra.bot, botPlaneDependencies(planes.infra),
		logger.With(slog.String("plane", "bot")))
	if err != nil {
		return planes, fmt.Errorf("build bot plane: %w", err)
	}

	planes.admin, err = adminruntime.BuildAdminAPIRuntime(ctx, appConfig.Admin, planes.infra.admin, adminPlaneDependencies(planes.infra, planes.bot),
		logger.With(slog.String("plane", "admin")))
	if err != nil {
		return planes, fmt.Errorf("build admin plane: %w", err)
	}

	planes.youtube, err = youtuberuntime.Build(ctx, &appConfig.YouTube, &appConfig.Bot.Postgres, planes.infra.bot.Cache, logger.With(slog.String("plane", "youtube")))
	if err != nil {
		return planes, fmt.Errorf("build youtube plane: %w", err)
	}

	if err := installAPIWorkerRegistry(ctx, appConfig, planes.bot, planes.youtube); err != nil {
		return planes, fmt.Errorf("build worker registry: %w", err)
	}

	return planes, nil
}

func botPlaneDependencies(infra *apiInfrastructure) botruntime2.Dependencies {
	return botruntime2.Dependencies{
		ACL:     infra.acl,
		Holodex: infra.holodex,
	}
}

func adminPlaneDependencies(infra *apiInfrastructure, bot *botruntime2.BotRuntime) adminruntime.Dependencies {
	return adminruntime.Dependencies{
		ACL:       infra.acl,
		Holodex:   infra.holodex,
		IrisRooms: bot.IrisRooms,
	}
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
		YouTube: planes.youtube,
	}

	runtime.group = newPlaneGroup(logger, apiPlaneComponents(planes)...)
	runtime.closeSteps = apiPlaneCloseSteps(planes)

	return runtime
}

func apiPlaneComponents(planes apiPlanes) []planeComponent {
	return []planeComponent{
		{Name: "youtube", Start: planes.youtube.Start, Shutdown: planes.youtube.Shutdown},
		{Name: "admin", Start: planes.admin.Start, Shutdown: planes.admin.Shutdown},
		{Name: "bot", Start: planes.bot.Start, Shutdown: planes.bot.Shutdown},
	}
}

func apiPlaneCloseSteps(planes apiPlanes) []func(context.Context) error {
	return []func(context.Context) error{
		planes.bot.CloseContext,
		planes.admin.CloseContext,
		planes.youtube.CloseContext,
		func(ctx context.Context) error {
			if !planes.bot.ResourceUsersJoined() || !planes.admin.ResourceUsersJoined() {
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
		OnError: func(err error) {
			r.Logger.Error("hololive-api runtime error", slog.String("error", sharedlogging.RedactDiagnostic(err.Error())))
		},
		BeforeShutdown: func() {
			r.Logger.Info("hololive-api draining runtime planes")
		},
		Shutdown: r.shutdownAndClose,
	}
}

func (r *Runtime) shutdownAndClose(ctx context.Context) error {
	r.stopOwnsCleanup.Store(true)

	done := make(chan error, 1)

	go func() { done <- r.drainAndClose(ctx) }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return errors.Join(fmt.Errorf("stop aggregate runtime: %w", ctx.Err()), r.closeError())
	}
}

func (r *Runtime) drainAndClose(ctx context.Context) error {
	drainCtx, cancel := context.WithTimeout(ctx, constants.AppTimeout.Shutdown)
	shutdownErr := r.Shutdown(drainCtx)

	cancel()

	if ctx.Err() != nil {
		return shutdownErr
	}

	return errors.Join(shutdownErr, r.CloseContext(ctx))
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}

	if r.stopOwnsCleanup.Load() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)

	defer cancel()

	if err := r.CloseContext(ctx); err != nil && r.Logger != nil {
		r.Logger.Error("aggregate runtime cleanup failed", slog.String("error", sharedlogging.RedactDiagnostic(err.Error())))
	}
}

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

type apiInfrastructure struct {
	bot, admin *sharedmodules.InfraModule
	acl        *acl.Service
	holodex    *holodexprovider.Service

	stopHolodex, stopMemberCache, closeAdmin, closeBot func()
	closeOnce                                          sync.Once
}

func (r *apiInfrastructure) CloseContext(context.Context) error {
	if r != nil {
		r.close()
	}

	return nil
}

func (r *apiInfrastructure) close() {
	r.closeOnce.Do(func() {
		for _, release := range []func(){r.stopHolodex, r.stopMemberCache, r.closeAdmin, r.closeBot} {
			if release != nil {
				release()
			}
		}
	})
}

func buildAPIInfrastructure(ctx context.Context, config *apiconfig.RuntimeConfig, logger *slog.Logger) (_ *apiInfrastructure, retErr error) {
	if config == nil || config.Bot == nil || config.Admin == nil {
		return nil, errors.New("bot and admin configs must not be nil")
	}

	bot, err := sharedmodules.BuildInfraModule(ctx, sharedmodules.InfraOptions{Valkey: config.Bot.Valkey, Postgres: config.Bot.Postgres, Pool: databaseproviders.RuntimePool{Name: "bot", ReservedConns: 1}}, logger)
	if err != nil {
		return nil, fmt.Errorf("build bot infrastructure: %w", err)
	}

	infra := &apiInfrastructure{
		bot:             &sharedmodules.InfraModule{Cache: bot.Cache, Postgres: bot.Postgres, MemberRepository: bot.MemberRepository, MemberCache: bot.MemberCache},
		stopMemberCache: bot.StopMemberCache,
		closeBot:        bot.Cleanup,
	}

	defer func() {
		if retErr != nil {
			infra.close()
		}
	}()

	admin, cleanupAdmin, err := databaseproviders.ProvideDatabaseResources(ctx, &config.Admin.Postgres, databaseproviders.RuntimePool{Name: "admin"}, logger)
	if err != nil {
		return nil, fmt.Errorf("provide admin database resources: %w", err)
	}

	infra.closeAdmin = cleanupAdmin
	infra.admin = &sharedmodules.InfraModule{Cache: infra.bot.Cache, Postgres: admin.Service, MemberRepository: member.NewMemberRepository(admin.Service), MemberCache: infra.bot.MemberCache}

	infra.acl, infra.holodex, err = buildSharedPlaneServices(ctx, config.Bot, infra.bot, logger)
	if err != nil {
		return nil, err
	}

	infra.stopHolodex = infra.holodex.Stop

	return infra, nil
}

func buildSharedPlaneServices(
	ctx context.Context,
	botConfig *apiconfig.BotPlaneConfig,
	bot *sharedmodules.InfraModule,
	logger *slog.Logger,
) (*acl.Service, *holodexprovider.Service, error) {
	aclMode, err := acl.ParseACLModeStrict(botConfig.Kakao.ACLMode)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid KAKAO_ACL_MODE: %w", err)
	}

	aclService, err := acl.NewACLService(ctx, bot.Postgres, logger, botConfig.Kakao.ACLEnabled, aclMode, botConfig.Kakao.Rooms)
	if err != nil {
		return nil, nil, fmt.Errorf("build ACL service: %w", err)
	}

	holodex, err := buildHolodexService(ctx, botConfig, bot.MemberCache, bot.Cache, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("build holodex service: %w", err)
	}

	return aclService, holodex, nil
}

func buildHolodexService(
	ctx context.Context,
	botConfig *apiconfig.BotPlaneConfig,
	memberCache *member.Cache,
	cacheClient holodexprovider.Cache,
	logger *slog.Logger,
) (*holodexprovider.Service, error) {
	scraperService, err := holodexproviders.ProvideScraperService(ctx, memberCache, logger, botConfig.OfficialSchedule)
	if err != nil {
		return nil, fmt.Errorf("provide scraper service: %w", err)
	}

	holodexService, err := holodexproviders.ProvideHolodexService(&botConfig.Holodex, cacheClient, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	return holodexService, nil
}
