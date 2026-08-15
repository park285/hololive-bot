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
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
	sharedlog "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

const (
	runtimeName       = "youtube-collector"
	runtimeAllowedEnv = "YOUTUBE_COLLECTOR_RUNTIME_ALLOWED"
)

type Runtime struct {
	Config          *settings.Config
	Logger          *slog.Logger
	Scheduler       *leaseScheduler
	servers         *sharedserver.RuntimeHTTPServers
	helper          *youtubejs.Helper
	infra           *collectorInfrastructure
	cleanupMu       sync.Mutex
	cleanupReported bool
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
		return nil, errors.Join(err, infra.Close())
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

	readiness := &collectorReadiness{appConfig: appConfig, infra: infra, scheduler: sched}
	router, err := sharedserver.NewHealthOnlyRuntimeRouter(
		ctx,
		logger,
		appConfig.Server.APIKey,
		readiness.configure,
	)
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
		helper:    infra.youtubejs,
		infra:     infra,
	}
	return runtime, nil
}

type collectorReadiness struct {
	appConfig *settings.Config
	infra     *collectorInfrastructure
	scheduler *leaseScheduler
}

func (r *collectorReadiness) configure(opts *sharedserver.RuntimeRouterOptions) {
	opts.EnableGzip = true
	opts.ReadyResponder = r.respond
}

func (r *collectorReadiness) respond(c *gin.Context) {
	writeCollectorReady(c, collectorInstanceID(r.appConfig), r.infra, r.scheduler)
}

func collectorInstanceID(appConfig *settings.Config) string {
	if appConfig == nil {
		return runtimeName
	}
	if id := strings.TrimSpace(appConfig.YouTubeCollector.InstanceID); id != "" {
		return id
	}
	return runtimeName
}

func writeCollectorReady(c *gin.Context, instanceID string, infra *collectorInfrastructure, sched *leaseScheduler) {
	if infra.youtubejs == nil || infra.youtubejs.Exited() || !infra.youtubejs.Healthy(c.Request.Context()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "runtime": runtimeName, "instance_id": instanceID, "dependency": "youtubejs"})
		return
	}
	snap := collectionReadySnapshot(c.Request.Context(), infra, sched)
	if dependency := snap.dependency(); dependency != "" {
		c.JSON(http.StatusServiceUnavailable, snap.payload(instanceID, dependency))
		return
	}
	c.JSON(http.StatusOK, snap.payload(instanceID, ""))
}

type collectionReady struct {
	firstSuccess    bool
	handoffComplete bool
	handoffStatus   string
	dueJobs         int
	pendingQueue    int
	pendingQueueOK  bool
}

func (s collectionReady) dependency() string {
	if !s.pendingQueueOK {
		return "postgres_queue"
	}
	if !s.firstSuccess {
		return "first_success"
	}
	if !s.handoffComplete {
		return "observation_handoff"
	}
	return ""
}

func collectionReadySnapshot(ctx context.Context, infra *collectorInfrastructure, sched *leaseScheduler) collectionReady {
	pending, err := pendingObservationCount(ctx, infra)
	snap := collectionReady{pendingQueue: pending, pendingQueueOK: err == nil}
	if sched == nil {
		return snap
	}
	snap.firstSuccess, snap.handoffComplete, snap.handoffStatus, err = collectionHandoffSnapshot(ctx, infra, sched.metrics)
	if err != nil {
		snap.pendingQueueOK = false
	}
	snap.dueJobs = sched.DueJobs()
	return snap
}

func collectionHandoffSnapshot(
	ctx context.Context,
	infra *collectorInfrastructure,
	metrics *Metrics,
) (hasFirstSuccess, handoffComplete bool, handoffStatus string, handoffErr error) {
	if metrics == nil {
		return false, false, "", nil
	}
	firstSuccess := metrics.HasSuccess()
	observationID, complete, ok := metrics.PublishedHandoff()
	if complete {
		return firstSuccess, true, "PROCESSED", nil
	}
	if !ok {
		return firstSuccess, false, "", nil
	}
	status, err := observationHandoffStatus(ctx, infra, observationID)
	if err != nil {
		return firstSuccess, false, "", err
	}
	complete = status == "PROCESSED"
	if complete {
		metrics.ObserveHandoffComplete(observationID)
	}
	return firstSuccess, complete, status, nil
}

func (s collectionReady) payload(instanceID, dependency string) gin.H {
	body := gin.H{
		"runtime": runtimeName, "instance_id": instanceID, "helper": "ok",
		"first_success": s.firstSuccess, "handoff_status": s.handoffStatus, "due_jobs": s.dueJobs,
	}
	if s.pendingQueueOK {
		body["pending_queue"] = s.pendingQueue
	} else {
		body["pending_queue"] = nil
	}
	if dependency != "" {
		body["status"] = "not_ready"
		body["dependency"] = dependency
		return body
	}
	body["status"] = "ready"
	return body
}

func observationHandoffStatus(ctx context.Context, infra *collectorInfrastructure, observationID int64) (string, error) {
	if infra == nil || infra.postgres == nil || infra.postgres.GetPool() == nil || observationID <= 0 {
		return "", fmt.Errorf("load observation handoff status: request is invalid")
	}
	var status string
	if err := infra.postgres.GetPool().QueryRow(ctx, mustSQL("observation_handoff_status.sql"), observationID).Scan(&status); err != nil {
		return "", fmt.Errorf("load observation handoff status: %w", err)
	}
	return status, nil
}

func pendingObservationCount(ctx context.Context, infra *collectorInfrastructure) (int, error) {
	if infra == nil || infra.postgres == nil || infra.postgres.GetPool() == nil {
		return 0, nil
	}
	var pending int
	err := infra.postgres.GetPool().QueryRow(ctx, mustSQL("pending_observation_count.sql")).Scan(&pending)
	if err != nil {
		return 0, fmt.Errorf("count pending source observations: %w", err)
	}
	return pending, nil
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
	r.watchHelper(ctx, errCh)
	r.startScheduler(ctx)
	r.startServers(errCh)
	sharedlog.Info(ctx, r.Logger, "youtube_collector_started", "youtube collector started",
		sharedlog.Runtime(runtimeName),
	)
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

func (r *Runtime) startScheduler(ctx context.Context) {
	if r.Scheduler == nil {
		return
	}
	r.Scheduler.Start(ctx)
	r.Logger.Info("Scraper scheduler started", slog.String("runtime", runtimeName))
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
	shutdownErr = errors.Join(shutdownErr, r.closeInfrastructure())
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
	if err := r.closeInfrastructure(); err != nil && r.Logger != nil {
		r.Logger.Error("YouTube collector cleanup failed", slog.Any("error", err))
	}
}

func (r *Runtime) closeInfrastructure() error {
	r.cleanupMu.Lock()
	defer r.cleanupMu.Unlock()
	if r.infra == nil {
		return nil
	}
	err := r.infra.Close()
	if err == nil || r.cleanupReported {
		return nil
	}
	r.cleanupReported = true
	return err
}
