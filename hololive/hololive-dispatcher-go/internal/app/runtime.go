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
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kapu/hololive-dispatcher-go/internal/dispatch"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	"github.com/kapu/hololive-shared/pkg/iris"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

const readyCheckTimeout = 2 * time.Second

// Runtime: dispatcher-go 실행 컨테이너.
type Runtime struct {
	cfg        *Config
	logger     *slog.Logger
	cacheSvc   cache.Client
	dispatcher *dispatch.Dispatcher
	httpServer *http.Server
	readyState *readinessState
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

// BuildRuntime: dispatcher-go 런타임 초기화.
func BuildRuntime(ctx context.Context, cfg *Config, logger *slog.Logger) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build runtime: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	cacheSvc, err := cache.NewCacheService(ctx, cfg.Valkey, logger)
	if err != nil {
		return nil, fmt.Errorf("build runtime: create cache service: %w", err)
	}

	consumer := queue.NewConsumer(
		cacheSvc,
		logger,
		queue.WithQueueKey(cfg.Dispatch.QueueKey),
		queue.WithMaxBatch(cfg.Dispatch.MaxBatch),
	)
	renderer := dispatch.NewSimpleRenderer()
	irisClient := iris.NewH2CClient(cfg.Iris.BaseURL, cfg.Iris.BotToken, logger)

	dispatcher, err := dispatch.NewDispatcher(
		consumer,
		irisClient,
		renderer,
		cfg.Dispatch.MaxBatch,
		cfg.Dispatch.Parallelism,
		logger,
	)
	if err != nil {
		_ = cacheSvc.Close()
		return nil, fmt.Errorf("build runtime: create dispatcher: %w", err)
	}

	runtime := &Runtime{
		cfg:        cfg,
		logger:     logger,
		cacheSvc:   cacheSvc,
		dispatcher: dispatcher,
		readyState: newReadinessState(),
	}

	runtime.httpServer = buildHTTPServer(cfg.Server.Port, runtime.routes())
	return runtime, nil
}

// Close: 런타임 리소스를 정리한다.
func (r *Runtime) Close() {
	if r == nil || r.cacheSvc == nil {
		return
	}
	if err := r.cacheSvc.Close(); err != nil {
		r.logger.Warn("Close cache service failed", slog.Any("error", err))
	}
}

// Run: dispatcher-go 메인 실행 루프.
func (r *Runtime) Run() {
	if r == nil {
		return
	}

	runCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dispatchCtx, dispatchCancel := context.WithCancel(runCtx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.runDispatchLoop(dispatchCtx)
	}()

	serverErrCh := make(chan error, 1)
	go func() {
		if err := r.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- fmt.Errorf("run runtime: http server: %w", err)
		}
	}()
	r.logger.Info("Dispatcher HTTP server started", slog.String("addr", r.httpServer.Addr))

	select {
	case <-runCtx.Done():
		r.logger.Info("Dispatcher shutdown signal received")
	case err := <-serverErrCh:
		r.logger.Error("Dispatcher HTTP server failed", slog.Any("error", err))
	}

	dispatchCancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)
	defer shutdownCancel()
	if err := r.httpServer.Shutdown(shutdownCtx); err != nil {
		r.logger.Error("Dispatcher HTTP shutdown failed", slog.Any("error", err))
	}
	wg.Wait()
}

func (r *Runtime) runDispatchLoop(ctx context.Context) {
	r.readyState.dispatchLoopRunning.Store(true)
	defer r.readyState.dispatchLoopRunning.Store(false)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("Dispatcher loop stopped")
			return
		default:
		}

		if err := r.dispatcher.RunOnce(ctx); err != nil {
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
	valkeyConnected := r.cacheSvc != nil && r.cacheSvc.IsConnected(checkCtx)

	ready := dispatchLoopRunning && valkeyConnected
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
	}

	writeJSON(req.Context(), w, statusCode, response)
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
