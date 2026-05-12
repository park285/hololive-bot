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
	"github.com/kapu/hololive-shared/pkg/health"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/park285/iris-client-go/iris"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
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

	cacheSvc, err := cache.NewCacheService(ctx, cfg.Valkey, logger)
	if err != nil {
		if cfg.Dispatch.ConsumerMode == "pg" {
			logger.Warn("Dispatch wakeup Valkey unavailable; PG fallback polling will be used", slog.Any("error", err))
		} else {
			return nil, fmt.Errorf("build runtime: create cache service: %w", err)
		}
	}
	var wakeupCacheSvc cache.Client
	if cfg.Dispatch.ConsumerMode == "pg" && cacheSvc != nil {
		wakeupCacheSvc, err = cache.NewCacheService(ctx, cfg.Valkey, logger)
		if err != nil {
			logger.Warn("Dispatch wakeup Valkey client unavailable; PG fallback polling will be used", slog.Any("error", err))
		}
	}

	var postgresSvc database.Client
	if cfg.Dispatch.ConsumerMode == "pg" {
		postgresSvc, err = database.NewPostgresService(ctx, cfg.Postgres, logger)
		if err != nil {
			if cacheSvc != nil {
				_ = cacheSvc.Close()
			}
			if wakeupCacheSvc != nil {
				_ = wakeupCacheSvc.Close()
			}
			return nil, fmt.Errorf("build runtime: create postgres service: %w", err)
		}
	}

	consumer := buildDispatchConsumer(cfg, cacheSvc, postgresSvc, logger)
	renderer := dispatch.NewSimpleRenderer()
	irisClient, err := sharedproviders.ProvideIrisClient(
		logger,
		iris.WithBaseURL(cfg.Iris.BaseURL),
		iris.WithBotToken(cfg.Iris.BotToken),
	)
	if err != nil {
		if cacheSvc != nil {
			_ = cacheSvc.Close()
		}
		if wakeupCacheSvc != nil {
			_ = wakeupCacheSvc.Close()
		}
		if postgresSvc != nil {
			_ = postgresSvc.Close()
		}
		return nil, fmt.Errorf("build runtime: create iris client: %w", err)
	}

	dispatcher, err := dispatch.NewDispatcher(
		consumer,
		irisClient,
		renderer,
		cfg.Dispatch.MaxBatch,
		cfg.Dispatch.Parallelism,
		logger,
	)
	if err != nil {
		if cacheSvc != nil {
			_ = cacheSvc.Close()
		}
		if wakeupCacheSvc != nil {
			_ = wakeupCacheSvc.Close()
		}
		if postgresSvc != nil {
			_ = postgresSvc.Close()
		}
		return nil, fmt.Errorf("build runtime: create dispatcher: %w", err)
	}
	if err := configureDispatcherRetryPolicy(dispatcher, cfg.Dispatch); err != nil {
		if cacheSvc != nil {
			_ = cacheSvc.Close()
		}
		if wakeupCacheSvc != nil {
			_ = wakeupCacheSvc.Close()
		}
		if postgresSvc != nil {
			_ = postgresSvc.Close()
		}
		return nil, fmt.Errorf("build runtime: configure dispatcher retry policy: %w", err)
	}
	if cfg.Dispatch.ConsumerMode == "pg" {
		dispatcher.ConfigureSendFailurePolicy(dispatch.SendFailurePolicyQuarantine)
	}

	runtime := &Runtime{
		cfg:            cfg,
		logger:         logger,
		cacheSvc:       cacheSvc,
		wakeupCacheSvc: wakeupCacheSvc,
		postgres:       postgresSvc,
		irisClient:     irisClient,
		dispatcher:     dispatcher,
		readyState:     newReadinessState(),
		irisProbe:      newCachedBoolProbe(2 * time.Second),
	}
	runtime.Managed = lifecycle.NewManaged(func() {
		if runtime.cacheSvc != nil {
			if err := runtime.cacheSvc.Close(); err != nil {
				runtime.logger.Warn("Close cache service failed", slog.Any("error", err))
			}
		}
		if runtime.wakeupCacheSvc != nil {
			if err := runtime.wakeupCacheSvc.Close(); err != nil {
				runtime.logger.Warn("Close wakeup cache service failed", slog.Any("error", err))
			}
		}
		if runtime.postgres != nil {
			if err := runtime.postgres.Close(); err != nil {
				runtime.logger.Warn("Close postgres service failed", slog.Any("error", err))
			}
		}
	})

	runtime.httpServer = buildHTTPServer(cfg.Server.Port, runtime.routes())
	return runtime, nil
}

func buildDispatchConsumer(
	cfg *Config,
	cacheSvc cache.Client,
	postgresSvc database.Client,
	logger *slog.Logger,
) interface {
	DrainBatch(context.Context, int) ([]domain.AlarmQueueEnvelope, error)
	MarkSending(context.Context, []domain.AlarmQueueEnvelope) error
	MarkDispatched(context.Context, []domain.AlarmQueueEnvelope) error
	ReleaseClaimKeys(context.Context, []string) error
	ScheduleRetry(context.Context, []domain.AlarmQueueEnvelope) error
	MoveToDLQ(context.Context, []domain.AlarmQueueEnvelope) error
	Requeue(context.Context, []domain.AlarmQueueEnvelope) error
	Quarantine(context.Context, []domain.AlarmQueueEnvelope, string) error
} {
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

	dispatchCancel := func() {}
	var wg sync.WaitGroup
	_ = lifecycle.Run(lifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start: func(ctx context.Context, errCh chan<- error) {
			dispatchCtx, cancel := context.WithCancel(ctx)
			dispatchCancel = cancel
			wg.Go(func() {
				r.runDispatchLoop(dispatchCtx)
			})

			go func() {
				if err := r.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errCh <- fmt.Errorf("run runtime: http server: %w", err)
				}
			}()
			r.logger.Info("Dispatcher HTTP server started", slog.String("addr", r.httpServer.Addr))
		},
		OnSignal: func(os.Signal) {
			r.logger.Info("Dispatcher shutdown signal received")
		},
		OnError: func(err error) {
			r.logger.Error("Dispatcher HTTP server failed", slog.Any("error", err))
		},
		Shutdown: func(ctx context.Context) error {
			dispatchCancel()
			defer wg.Wait()

			if err := r.httpServer.Shutdown(ctx); err != nil {
				r.logger.Error("Dispatcher HTTP shutdown failed", slog.Any("error", err))
				return err
			}
			return nil
		},
	})
}

func (r *Runtime) runDispatchLoop(ctx context.Context) {
	r.readyState.dispatchLoopRunning.Store(true)
	defer r.readyState.dispatchLoopRunning.Store(false)

	batchesSinceWait := 0
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("Dispatcher loop stopped")
			return
		default:
		}

		processed, err := r.dispatcher.RunOnceProcessed(ctx)
		if err != nil {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				r.readyState.clearLastError()
				r.logger.Info("Dispatcher loop stopped")
				return
			}

			r.readyState.setLastError(err.Error())
			r.logger.Warn("Dispatch loop iteration failed", slog.Any("error", err))

			timer := time.NewTimer(r.cfg.Dispatch.ReconnectBackoff)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
			continue
		}

		r.readyState.clearLastError()
		if processed && r.cfg != nil && r.cfg.Dispatch.ConsumerMode == "pg" {
			batchesSinceWait++
			maxBatches := r.cfg.Dispatch.MaxBatchesPerWake
			if maxBatches <= 0 {
				maxBatches = defaultMaxBatchesPerWake
			}
			if batchesSinceWait >= maxBatches {
				batchesSinceWait = 0
				if !sleepContext(ctx, 10*time.Millisecond) {
					r.logger.Info("Dispatcher loop stopped")
					return
				}
			}
			continue
		}
		if !processed && r.cfg != nil && r.cfg.Dispatch.ConsumerMode == "pg" {
			batchesSinceWait = 0
			if !r.waitForPGDispatchSignal(ctx) {
				r.logger.Info("Dispatcher loop stopped")
				return
			}
		}
	}
}

func (r *Runtime) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", r.handleHealth)
	mux.HandleFunc("/ready", r.handleReady)
	return mux
}

func (r *Runtime) handleHealth(w http.ResponseWriter, req *http.Request) {
	writeJSON(req.Context(), w, http.StatusOK, health.Get())
}

func (r *Runtime) handleReady(w http.ResponseWriter, req *http.Request) {
	dispatchLoopRunning := r.readyState.dispatchLoopRunning.Load()

	checkCtx, cancel := context.WithTimeout(req.Context(), readyCheckTimeout)
	defer cancel()
	irisConnected := r.cachedIrisPing(checkCtx)
	consumerMode := "valkey"
	if r.cfg != nil && r.cfg.Dispatch.ConsumerMode != "" {
		consumerMode = r.cfg.Dispatch.ConsumerMode
	}
	valkeyConnected := r.cacheSvc != nil && r.cacheSvc.IsConnected(checkCtx)
	wakeupConnected := valkeyConnected
	if consumerMode == "pg" {
		wakeupConnected = r.wakeupCacheSvc != nil && r.wakeupCacheSvc.IsConnected(checkCtx)
	}
	postgresConnected := consumerMode != "pg"
	if consumerMode == "pg" {
		postgresConnected = r.postgres != nil && r.postgres.Ping(checkCtx) == nil
	}
	wakeupEnabled := true
	if r.cfg != nil {
		wakeupEnabled = r.cfg.Dispatch.WakeupEnabled
	}
	wakeupDegraded := consumerMode == "pg" && (!wakeupEnabled || !wakeupConnected)

	valkeyRequired := consumerMode != "pg"
	ready := dispatchLoopRunning && irisConnected && postgresConnected && (!valkeyRequired || valkeyConnected)
	statusCode := http.StatusOK
	status := "ready"
	if !ready {
		statusCode = http.StatusServiceUnavailable
		status = "not_ready"
	}

	response := map[string]any{
		"status":                status,
		"dispatch_loop_running": dispatchLoopRunning,
		"valkey_connected":      valkeyConnected,
		"wakeup_degraded":       wakeupDegraded,
		"iris_connected":        irisConnected,
		"postgres_connected":    postgresConnected,
		"consumer_mode":         consumerMode,
	}

	writeJSON(req.Context(), w, statusCode, response)
}

func (r *Runtime) cachedIrisPing(ctx context.Context) bool {
	if r == nil || r.irisClient == nil {
		return false
	}

	if r.irisProbe == nil {
		return r.irisClient.Ping(ctx)
	}

	return r.irisProbe.Get(ctx, func(ctx context.Context) bool {
		return r.irisClient.Ping(ctx)
	})
}

type cachedBoolProbe struct {
	mu     sync.Mutex
	ttl    time.Duration
	lastAt time.Time
	lastOK bool
}

func newCachedBoolProbe(ttl time.Duration) *cachedBoolProbe {
	if ttl <= 0 {
		ttl = time.Second
	}

	return &cachedBoolProbe{ttl: ttl}
}

func (p *cachedBoolProbe) Get(ctx context.Context, fn func(context.Context) bool) bool {
	if p == nil || fn == nil {
		return false
	}

	now := time.Now()

	p.mu.Lock()
	if !p.lastAt.IsZero() && now.Sub(p.lastAt) < p.ttl {
		result := p.lastOK
		p.mu.Unlock()
		return result
	}
	p.mu.Unlock()

	result := fn(ctx)

	p.mu.Lock()
	p.lastAt = now
	p.lastOK = result
	p.mu.Unlock()

	return result
}

func buildHTTPServer(port int, handler http.Handler) *http.Server {
	addr := fmt.Sprintf(":%d", port)
	return &http.Server{
		Addr:              addr,
		Handler:           sharedserver.WrapH2C(handler),
		ReadHeaderTimeout: constants.ServerTimeout.ReadHeader,
		ReadTimeout:       constants.ServerTimeout.Read,
		WriteTimeout:      constants.ServerTimeout.Write,
		IdleTimeout:       constants.ServerTimeout.Idle,
		MaxHeaderBytes:    constants.ServerTimeout.MaxHeaderBytes,
	}
}

func writeJSON(ctx context.Context, w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Default().WarnContext(ctx, "Write JSON response failed", slog.Any("error", err))
	}
}
