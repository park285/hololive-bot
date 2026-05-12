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
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

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
		valkeyConnected = wakeupConnected
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
