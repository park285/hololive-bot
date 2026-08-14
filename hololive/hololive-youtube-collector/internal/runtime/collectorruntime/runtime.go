package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	sharedlog "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

const (
	runtimeName       = "youtube-collector"
	runtimeAllowedEnv = "YOUTUBE_COLLECTOR_RUNTIME_ALLOWED"
)

type Runtime struct {
	Config    *settings.Config
	Logger    *slog.Logger
	Scheduler *leaseScheduler
	servers   *sharedserver.RuntimeHTTPServers
	cleanup   func()
	lifecycle.Managed
}

func Build(ctx context.Context, appConfig *settings.Config, logger *slog.Logger) (*Runtime, error) {
	if appConfig == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	if !runtimeAllowed() {
		return nil, fmt.Errorf("youtube collector runtime disabled: set %s=true on the owning host", runtimeAllowedEnv)
	}
	if !appConfig.Ingestion.YouTubeEnabled {
		return nil, fmt.Errorf("youtube collector requires YOUTUBE_INGESTION_ENABLED=true")
	}

	infra, err := initInfrastructure(ctx, appConfig, logger)
	if err != nil {
		return nil, err
	}

	runtime, err := assembleRuntime(ctx, appConfig, logger, infra)
	if err != nil {
		infra.cleanup()
		return nil, err
	}
	return runtime, nil
}

func runtimeAllowed() bool {
	allowed, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(runtimeAllowedEnv)))
	return err == nil && allowed
}

func assembleRuntime(
	ctx context.Context,
	appConfig *settings.Config,
	logger *slog.Logger,
	infra *collectorInfrastructure,
) (*Runtime, error) {
	sched, err := buildScheduler(appConfig, infra, logger)
	if err != nil {
		return nil, err
	}

	router, err := sharedserver.NewHealthOnlyRuntimeRouter(ctx, logger, appConfig.Server.APIKey, func(opts *sharedserver.RuntimeRouterOptions) {
		opts.EnableGzip = true
		opts.ReadyResponder = func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ready", "runtime": runtimeName})
		}
	})
	if err != nil {
		return nil, fmt.Errorf("build youtube collector router: %w", err)
	}
	servers, err := sharedserver.NewRuntimeHTTPServers(ctx, &appConfig.Server, router, runtimeName+"-http")
	if err != nil {
		return nil, err
	}

	runtime := &Runtime{
		Config:    appConfig,
		Logger:    logger,
		Scheduler: sched,
		servers:   servers,
		cleanup:   infra.cleanup,
	}
	runtime.Managed = lifecycle.NewManaged(infra.cleanup)
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
	if r.Scheduler != nil {
		r.Scheduler.Start(ctx)
		r.Logger.Info("Scraper scheduler started", slog.String("runtime", runtimeName))
	}
	if r.servers != nil {
		r.servers.Start(r.Logger, errCh)
		r.Logger.Info("YouTube collector HTTP server started",
			slog.String("runtime", runtimeName),
			slog.String("addr", r.servers.Addr()),
		)
	}
	sharedlog.Info(ctx, r.Logger, "youtube_collector_started", "youtube collector started",
		sharedlog.Runtime(runtimeName),
	)
}

func (r *Runtime) shutdown(ctx context.Context) error {
	if r.Scheduler != nil {
		r.Scheduler.Stop()
	}
	if r.servers != nil {
		if err := r.servers.Shutdown(ctx); err != nil {
			r.Logger.Error("YouTube collector HTTP shutdown failed", slog.Any("error", err))
		}
	}
	sharedlog.Info(ctx, r.Logger, "youtube_collector_stopped", "youtube collector stopped",
		sharedlog.Runtime(runtimeName),
	)
	return nil
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.Managed.Close()
}
