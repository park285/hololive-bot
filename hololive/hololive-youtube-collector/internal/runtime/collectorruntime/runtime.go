package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
	sharedlog "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/park285/shared-go/v2/pkg/workercontract"
)

const (
	runtimeName       = "youtube-collector"
	runtimeAllowedEnv = "YOUTUBE_COLLECTOR_RUNTIME_ALLOWED"
)

type Runtime struct {
	Config          *settings.YouTubeCollectorRuntimeConfig
	Logger          *slog.Logger
	Scheduler       *leaseScheduler
	servers         *sharedserver.RuntimeHTTPServers
	helper          *youtubejs.Helper
	infra           *collectorInfrastructure
	workerRegistry  *workercontract.Registry
	profileChecker  *workercontract.ProfileFileChecker
	cleanupMu       sync.Mutex
	cleanupReported bool
}

func Build(ctx context.Context, appConfig *settings.YouTubeCollectorRuntimeConfig, logger *slog.Logger) (*Runtime, error) {
	if appConfig == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	if !appConfig.RuntimeOwnership.RuntimeAllowed {
		return nil, fmt.Errorf("youtube collector runtime disabled: set %s=true on the owning host", runtimeAllowedEnv)
	}
	if appConfig.WorkerProfile == nil {
		return nil, fmt.Errorf("youtube collector worker profile is required")
	}
	if !collectorProfileEnabled(appConfig.WorkerProfile) {
		return assembleDisabledRuntime(ctx, appConfig, logger)
	}

	infra, err := initInfrastructure(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}

	runtime, err := assembleRuntime(ctx, appConfig, logger, infra)
	if err != nil {
		return nil, errors.Join(err, infra.Close(ctx))
	}
	return runtime, nil
}

func assembleDisabledRuntime(
	ctx context.Context,
	appConfig *settings.YouTubeCollectorRuntimeConfig,
	logger *slog.Logger,
) (*Runtime, error) {
	workerRegistry, profileChecker, err := newCollectorWorkerRegistry(appConfig.WorkerProfile, nil)
	if err != nil {
		return nil, err
	}
	readiness := &collectorReadiness{appConfig: appConfig, disabled: true}
	router, err := sharedserver.NewHealthOnlyRuntimeRouter(ctx, logger, appConfig.Server.APIKey, func(options *sharedserver.RuntimeRouterOptions) {
		readiness.configure(options)
		options.WorkerRegistry = workerRegistry
	})
	if err != nil {
		return nil, fmt.Errorf("build disabled youtube collector router: %w", err)
	}
	servers, err := sharedserver.NewRuntimeHTTPServers(ctx, &appConfig.Server, router, runtimeName+"-http", workerRegistry)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		Config: appConfig, Logger: logger, servers: servers,
		workerRegistry: workerRegistry, profileChecker: profileChecker,
	}, nil
}

func assembleRuntime(
	ctx context.Context,
	appConfig *settings.YouTubeCollectorRuntimeConfig,
	logger *slog.Logger,
	infra *collectorInfrastructure,
) (*Runtime, error) {
	sched, err := buildScheduler(appConfig, infra, logger)
	if err != nil {
		return nil, err
	}
	workerRegistry, profileChecker, err := newCollectorWorkerRegistry(appConfig.WorkerProfile, sched)
	if err != nil {
		return nil, err
	}

	readiness := &collectorReadiness{appConfig: appConfig, infra: infra, scheduler: sched}
	router, err := sharedserver.NewHealthOnlyRuntimeRouter(
		ctx,
		logger,
		appConfig.Server.APIKey,
		func(options *sharedserver.RuntimeRouterOptions) {
			readiness.configure(options)
			options.WorkerRegistry = workerRegistry
		},
	)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector router: %w", err)
	}
	servers, err := sharedserver.NewRuntimeHTTPServers(ctx, &appConfig.Server, router, runtimeName+"-http", workerRegistry)
	if err != nil {
		return nil, err
	}

	runtime := &Runtime{
		Config:         appConfig,
		Logger:         logger,
		Scheduler:      sched,
		servers:        servers,
		helper:         infra.youtubejs,
		infra:          infra,
		workerRegistry: workerRegistry,
		profileChecker: profileChecker,
	}
	return runtime, nil
}

func (r *Runtime) Run() error {
	var runtimeErr error
	err := lifecycle.Run(lifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start:           r.start,
		OnSignal: func(sig os.Signal) {
			r.Logger.Info("Received shutdown signal",
				slog.String("runtime", runtimeName),
				slog.String("signal", sig.String()),
			)
		},
		OnError: func(err error) {
			runtimeErr = err
			r.Logger.Error("Server error", slog.String("runtime", runtimeName), slog.Any("error", err))
		},
		Shutdown: r.shutdown,
	})
	if runtimeErr != nil && !errors.Is(err, runtimeErr) {
		err = errors.Join(runtimeErr, err)
	}
	return err
}

func (r *Runtime) start(ctx context.Context, errCh chan<- error) {
	if r.profileChecker != nil {
		panicguard.Go(r.Logger, "youtube-collector-profile-checker", func() {
			r.profileChecker.Run(ctx)
		})
	}
	r.watchHelper(ctx, errCh)
	r.startScheduler(ctx, errCh)
	r.watchSchedulerFatal(ctx, errCh)
	r.startServers(errCh)
	sharedlog.Info(ctx, r.Logger, "youtube_collector_started", "youtube collector started",
		sharedlog.Runtime(runtimeName),
	)
}

func (r *Runtime) watchSchedulerFatal(ctx context.Context, errCh chan<- error) {
	if r.Scheduler == nil || !r.collectionExecutorEnabled() {
		return
	}
	fatal := r.Scheduler.Fatal()
	if fatal == nil {
		return
	}
	panicguard.Go(r.Logger, "youtube-collector-scheduler-fatal", func() {
		err := receiveSchedulerFatal(ctx, fatal)
		if err != nil {
			forwardRuntimeError(ctx, errCh, err)
		}
	})
}

func receiveSchedulerFatal(ctx context.Context, fatal <-chan error) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-fatal:
		return err
	}
}

func forwardRuntimeError(ctx context.Context, errCh chan<- error, err error) {
	select {
	case errCh <- err:
	case <-ctx.Done():
	}
}

func (r *Runtime) watchHelper(ctx context.Context, errCh chan<- error) {
	if r.helper == nil {
		return
	}
	panicguard.Go(r.Logger, "youtubejs-helper-exit", func() {
		r.forwardHelperExit(ctx, errCh)
	})
}

func (r *Runtime) forwardHelperExit(ctx context.Context, errCh chan<- error) {
	select {
	case <-ctx.Done():
		return
	case <-r.helper.Done():
		r.reportHelperExit(ctx, errCh)
	}
}

func (r *Runtime) reportHelperExit(ctx context.Context, errCh chan<- error) {
	if ctx.Err() != nil {
		return
	}
	err := r.helper.ExitError()
	if err == nil {
		err = fmt.Errorf("youtube.js helper exited")
	} else {
		err = fmt.Errorf("youtube.js helper exited: %w", err)
	}
	select {
	case errCh <- err:
	case <-ctx.Done():
	}
}

func (r *Runtime) startScheduler(ctx context.Context, errCh chan<- error) {
	if r.Scheduler == nil || !r.collectionExecutorEnabled() {
		return
	}
	if err := r.Scheduler.Start(ctx); err != nil {
		forwardRuntimeError(ctx, errCh, err)
		return
	}
	r.Logger.Info("Scraper scheduler started", slog.String("runtime", runtimeName))
}

func (r *Runtime) collectionExecutorEnabled() bool {
	if r == nil || r.Config == nil || r.Config.WorkerProfile == nil {
		return false
	}
	worker, ok := r.Config.WorkerProfile.Loaded.Profile.Workers["collection"]
	return ok && worker.Executor.Enabled
}

func collectorProfileEnabled(profile *settings.YouTubeCollectorWorkerProfile) bool {
	if profile == nil {
		return false
	}
	worker, ok := profile.Loaded.Profile.Workers["collection"]
	return ok && worker.Executor.Enabled
}

func (r *Runtime) startServers(errCh chan<- error) {
	if r.servers == nil {
		return
	}
	r.servers.Start(r.Logger, errCh)
	r.Logger.Info("YouTube collector HTTP server started",
		slog.String("runtime", runtimeName),
		slog.String("addr", r.servers.Addr()),
	)
}

func (r *Runtime) shutdown(ctx context.Context) error {
	var shutdownErr error
	if r.Scheduler != nil {
		shutdownErr = errors.Join(shutdownErr, r.Scheduler.Stop(ctx))
	}
	if r.servers != nil {
		if err := r.servers.Shutdown(ctx); err != nil {
			r.Logger.Error("YouTube collector HTTP shutdown failed", slog.Any("error", err))
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}
	shutdownErr = errors.Join(shutdownErr, r.closeInfrastructure(ctx))
	if shutdownErr == nil {
		sharedlog.Info(ctx, r.Logger, "youtube_collector_stopped", "youtube collector stopped",
			sharedlog.Runtime(runtimeName),
		)
	}
	return shutdownErr
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	if err := r.closeInfrastructure(context.Background()); err != nil && r.Logger != nil {
		r.Logger.Error("YouTube collector cleanup failed", slog.Any("error", err))
	}
}

func (r *Runtime) closeInfrastructure(ctx context.Context) error {
	r.cleanupMu.Lock()
	defer r.cleanupMu.Unlock()
	if r.infra == nil {
		return nil
	}
	err := r.infra.Close(ctx)
	if err == nil || r.cleanupReported {
		return nil
	}
	r.cleanupReported = true
	return err
}
