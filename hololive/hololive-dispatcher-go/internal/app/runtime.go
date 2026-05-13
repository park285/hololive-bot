// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-dispatcher-go/internal/dispatch"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/park285/iris-client-go/iris"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

const readyCheckTimeout = 500 * time.Millisecond

type Runtime struct {
	cfg            *Config
	logger         *slog.Logger
	cacheSvc       cache.Client
	wakeupCacheSvc cache.Client
	postgres       database.Client
	irisClient     interface {
		Ping(ctx context.Context) bool
	}
	dispatcher *dispatch.Dispatcher
	httpServer *http.Server
	readyState *readinessState
	irisProbe  *cachedBoolProbe
	lifecycle.Managed
}

type dispatchConsumer interface {
	DrainBatch(context.Context, int) ([]domain.AlarmQueueEnvelope, error)
	MarkSending(context.Context, []domain.AlarmQueueEnvelope) error
	MarkDispatched(context.Context, []domain.AlarmQueueEnvelope) error
	ReleaseClaimKeys(context.Context, []string) error
	ScheduleRetry(context.Context, []domain.AlarmQueueEnvelope) error
	MoveToDLQ(context.Context, []domain.AlarmQueueEnvelope) error
	Requeue(context.Context, []domain.AlarmQueueEnvelope) error
	Quarantine(context.Context, []domain.AlarmQueueEnvelope, string) error
}

type runtimeIrisClient interface {
	Ping(ctx context.Context) bool
	SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error
}

type runtimeResources struct {
	cacheSvc       cache.Client
	wakeupCacheSvc cache.Client
	postgres       database.Client
}

type readinessState struct {
	dispatchLoopRunning atomic.Bool
	lastError           atomic.Value // string
}

func newReadinessState() *readinessState {
	state := &readinessState{}
	state.lastError.Store("")
	return state
}

func (s *readinessState) setLastError(message string) {
	s.lastError.Store(message)
}

func (s *readinessState) clearLastError() {
	s.lastError.Store("")
}

func (s *readinessState) getLastError() string {
	value, _ := s.lastError.Load().(string)
	return value
}

func BuildRuntime(ctx context.Context, cfg *Config, logger *slog.Logger) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build runtime: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	resources, err := buildRuntimeResources(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	irisClient, err := provideRuntimeIrisClient(cfg, logger)
	if err != nil {
		resources.close(logger)
		return nil, err
	}

	dispatcher, err := buildRuntimeDispatcher(cfg, resources, irisClient, logger)
	if err != nil {
		resources.close(logger)
		return nil, err
	}

	runtime := newRuntime(cfg, logger, resources, irisClient, dispatcher)
	runtime.Managed = lifecycle.NewManaged(runtime.closeResources)
	runtime.httpServer = buildHTTPServer(cfg.Server.Port, runtime.routes())
	return runtime, nil
}

func buildRuntimeResources(ctx context.Context, cfg *Config, logger *slog.Logger) (runtimeResources, error) {
	if cfg.Dispatch.ConsumerMode == "pg" {
		return buildPGRuntimeResources(ctx, cfg, logger)
	}

	cacheSvc, err := cache.NewCacheService(ctx, cfg.Valkey, logger)
	if err != nil {
		return runtimeResources{}, fmt.Errorf("build runtime: create cache service: %w", err)
	}
	return runtimeResources{cacheSvc: cacheSvc}, nil
}

func buildPGRuntimeResources(ctx context.Context, cfg *Config, logger *slog.Logger) (runtimeResources, error) {
	resources := runtimeResources{}
	if cfg.Dispatch.WakeupEnabled {
		wakeupCacheSvc, err := cache.NewCacheService(ctx, cfg.Valkey, logger)
		if err != nil {
			logger.Warn("Dispatch wakeup Valkey client unavailable; PG fallback polling will be used", slog.Any("error", err))
		} else {
			resources.wakeupCacheSvc = wakeupCacheSvc
		}
	}

	postgresSvc, err := database.NewPostgresService(ctx, cfg.Postgres, logger)
	if err != nil {
		resources.close(logger)
		return runtimeResources{}, fmt.Errorf("build runtime: create postgres service: %w", err)
	}
	resources.postgres = postgresSvc
	return resources, nil
}

func (r runtimeResources) close(logger *slog.Logger) {
	closeRuntimeResource(logger, "cache service", r.cacheSvc)
	closeRuntimeResource(logger, "wakeup cache service", r.wakeupCacheSvc)
	closeRuntimeResource(logger, "postgres service", r.postgres)
}

func closeRuntimeResource(logger *slog.Logger, name string, resource interface{ Close() error }) {
	if resource == nil {
		return
	}
	if err := resource.Close(); err != nil {
		logger.Warn("Close "+name+" failed", slog.Any("error", err))
	}
}

func provideRuntimeIrisClient(cfg *Config, logger *slog.Logger) (runtimeIrisClient, error) {
	irisClient, err := sharedproviders.ProvideIrisClient(
		logger,
		iris.WithBaseURL(cfg.Iris.BaseURL),
		iris.WithBotToken(cfg.Iris.BotToken),
	)
	if err != nil {
		return nil, fmt.Errorf("build runtime: create iris client: %w", err)
	}
	return irisClient, nil
}

func buildRuntimeDispatcher(
	cfg *Config,
	resources runtimeResources,
	irisClient runtimeIrisClient,
	logger *slog.Logger,
) (*dispatch.Dispatcher, error) {
	consumer := buildDispatchConsumer(cfg, resources.cacheSvc, resources.postgres, logger)
	dispatcher, err := dispatch.NewDispatcher(
		consumer,
		irisClient,
		dispatch.NewSimpleRenderer(),
		cfg.Dispatch.MaxBatch,
		cfg.Dispatch.Parallelism,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("build runtime: create dispatcher: %w", err)
	}
	if err := configureRuntimeDispatcher(dispatcher, cfg.Dispatch); err != nil {
		return nil, err
	}
	return dispatcher, nil
}

func configureRuntimeDispatcher(dispatcher *dispatch.Dispatcher, cfg DispatchConfig) error {
	if err := configureDispatcherRetryPolicy(dispatcher, cfg); err != nil {
		return fmt.Errorf("build runtime: configure dispatcher retry policy: %w", err)
	}
	if cfg.ConsumerMode == "pg" {
		dispatcher.ConfigureSendFailurePolicy(dispatch.SendFailurePolicyQuarantine)
	}
	return nil
}

func newRuntime(
	cfg *Config,
	logger *slog.Logger,
	resources runtimeResources,
	irisClient interface {
		Ping(ctx context.Context) bool
	},
	dispatcher *dispatch.Dispatcher,
) *Runtime {
	return &Runtime{
		cfg:            cfg,
		logger:         logger,
		cacheSvc:       resources.cacheSvc,
		wakeupCacheSvc: resources.wakeupCacheSvc,
		postgres:       resources.postgres,
		irisClient:     irisClient,
		dispatcher:     dispatcher,
		readyState:     newReadinessState(),
		irisProbe:      newCachedBoolProbe(2 * time.Second),
	}
}

func (r *Runtime) closeResources() {
	runtimeResources{
		cacheSvc:       r.cacheSvc,
		wakeupCacheSvc: r.wakeupCacheSvc,
		postgres:       r.postgres,
	}.close(r.logger)
}

func buildDispatchConsumer(
	cfg *Config,
	cacheSvc cache.Client,
	postgresSvc database.Client,
	logger *slog.Logger,
) dispatchConsumer {
	if cfg.Dispatch.ConsumerMode == "pg" {
		repo := dispatchoutbox.NewPgxRepository(postgresSvc)
		return dispatchoutbox.NewConsumer(
			repo,
			logger,
			dispatchoutbox.WithLease(time.Duration(cfg.Dispatch.LeaseSeconds)*time.Second),
			dispatchoutbox.WithRecoveryInterval(cfg.Dispatch.RecoveryInterval),
			dispatchoutbox.WithRecoveryBatchSize(cfg.Dispatch.RecoveryBatchSize),
		)
	}
	return queue.NewConsumer(
		cacheSvc,
		logger,
		queue.WithQueueKey(cfg.Dispatch.QueueKey),
		queue.WithMaxBatch(cfg.Dispatch.MaxBatch),
	)
}

func configureDispatcherRetryPolicy(dispatcher *dispatch.Dispatcher, cfg DispatchConfig) error {
	if dispatcher == nil {
		return fmt.Errorf("configure dispatcher retry policy: dispatcher is nil")
	}
	return dispatcher.ConfigureRetryPolicy(dispatch.RetryPolicy{
		MaxAttempts:   cfg.RetryMaxAttempts,
		BaseBackoff:   cfg.RetryBaseBackoff,
		MaxBackoff:    cfg.RetryMaxBackoff,
		JitterPercent: cfg.RetryJitterPercent,
	})
}

func (r *Runtime) Run() {
	if r == nil {
		return
	}

	runner := newRuntimeRunner(r)
	_ = lifecycle.Run(lifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start:           runner.start,
		OnSignal: func(os.Signal) {
			r.logger.Info("Dispatcher shutdown signal received")
		},
		OnError: func(err error) {
			r.logger.Error("Dispatcher HTTP server failed", slog.Any("error", err))
		},
		Shutdown: runner.shutdown,
	})
}

type runtimeRunner struct {
	r              *Runtime
	dispatchCancel func()
	wg             sync.WaitGroup
}

func newRuntimeRunner(r *Runtime) *runtimeRunner {
	return &runtimeRunner{r: r, dispatchCancel: func() {}}
}

func (rr *runtimeRunner) start(ctx context.Context, errCh chan<- error) {
	dispatchCtx, cancel := context.WithCancel(ctx)
	rr.dispatchCancel = cancel
	rr.wg.Go(func() {
		rr.r.runDispatchLoop(dispatchCtx)
	})

	go rr.serveHTTP(errCh)
	rr.r.logger.Info("Dispatcher HTTP server started", slog.String("addr", rr.r.httpServer.Addr))
}

func (rr *runtimeRunner) serveHTTP(errCh chan<- error) {
	if err := rr.r.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- fmt.Errorf("run runtime: http server: %w", err)
	}
}

func (rr *runtimeRunner) shutdown(ctx context.Context) error {
	rr.dispatchCancel()
	defer rr.wg.Wait()

	if err := rr.r.httpServer.Shutdown(ctx); err != nil {
		rr.r.logger.Error("Dispatcher HTTP shutdown failed", slog.Any("error", err))
		return err
	}
	return nil
}

func (r *Runtime) runDispatchLoop(ctx context.Context) {
	r.readyState.dispatchLoopRunning.Store(true)
	defer r.readyState.dispatchLoopRunning.Store(false)

	state := dispatchLoopState{}
	for {
		if !r.runDispatchLoopStep(ctx, &state) {
			return
		}
	}
}

type dispatchLoopState struct {
	batchesSinceWait int
}

func (r *Runtime) runDispatchLoopStep(ctx context.Context, state *dispatchLoopState) bool {
	if r.dispatchContextDone(ctx, true) {
		return false
	}

	processed, err := r.dispatcher.RunOnceProcessed(ctx)
	if err != nil {
		return r.handleDispatchLoopError(ctx, err)
	}

	r.readyState.clearLastError()
	if processed {
		return r.handleDispatchProcessed(ctx, state)
	}
	return r.handleDispatchIdle(ctx, state)
}

func (r *Runtime) dispatchContextDone(ctx context.Context, logStopped bool) bool {
	select {
	case <-ctx.Done():
		if logStopped {
			r.logger.Info("Dispatcher loop stopped")
		}
		return true
	default:
		return false
	}
}

func (r *Runtime) handleDispatchLoopError(ctx context.Context, err error) bool {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		r.readyState.clearLastError()
		r.logger.Info("Dispatcher loop stopped")
		return false
	}

	r.readyState.setLastError(err.Error())
	r.logger.Warn("Dispatch loop iteration failed", slog.Any("error", err))
	return sleepContext(ctx, r.cfg.Dispatch.ReconnectBackoff)
}

func (r *Runtime) handleDispatchProcessed(ctx context.Context, state *dispatchLoopState) bool {
	if !r.pgConsumerMode() {
		return true
	}

	state.batchesSinceWait++
	if state.batchesSinceWait < r.maxBatchesPerWake() {
		return true
	}

	state.batchesSinceWait = 0
	if sleepContext(ctx, 10*time.Millisecond) {
		return true
	}
	r.logger.Info("Dispatcher loop stopped")
	return false
}

func (r *Runtime) handleDispatchIdle(ctx context.Context, state *dispatchLoopState) bool {
	if !r.pgConsumerMode() {
		return true
	}

	state.batchesSinceWait = 0
	if r.waitForPGDispatchSignal(ctx) {
		return true
	}
	r.logger.Info("Dispatcher loop stopped")
	return false
}

func (r *Runtime) pgConsumerMode() bool {
	return r.cfg != nil && r.cfg.Dispatch.ConsumerMode == "pg"
}

func (r *Runtime) maxBatchesPerWake() int {
	maxBatches := r.cfg.Dispatch.MaxBatchesPerWake
	if maxBatches <= 0 {
		return defaultMaxBatchesPerWake
	}
	return maxBatches
}
