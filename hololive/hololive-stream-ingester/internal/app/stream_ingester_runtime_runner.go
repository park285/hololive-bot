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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

// StreamIngesterRuntime: stream-ingester 전용 런타임 (YouTube/스크래퍼/PhotoSync/Outbox).
type StreamIngesterRuntime struct {
	RuntimeName string
	Config      *config.Config
	Logger      *slog.Logger

	Scheduler        youtube.Scheduler
	ScraperScheduler *poller.Scheduler
	PhotoSync        *holodex.PhotoSyncService
	OutboxDispatcher *outbox.Dispatcher
	ConfigSubscriber *configsub.Subscriber

	ServerAddr string
	HttpServer *http.Server

	Readiness *ingestionReadinessState

	ingestionLease *providers.IngestionLease
	cleanup        func()
}

// Close: 리소스를 정리합니다.
func (r *StreamIngesterRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

func (r *StreamIngesterRuntime) runtimeName() string {
	if r == nil {
		return streamIngesterRuntimeName
	}
	name := strings.TrimSpace(r.RuntimeName)
	if name == "" {
		return streamIngesterRuntimeName
	}
	return name
}

// Run: SIGINT/SIGTERM 신호를 대기하며 graceful shutdown을 수행합니다. (블로킹)
func (r *StreamIngesterRuntime) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)
	r.startBackgroundServices(ctx, errCh)
	r.startHTTPServer(errCh)
	if r.Readiness != nil {
		r.Readiness.markRunning()
	}

	r.waitForShutdown(sigCh, errCh)

	cancel()
	r.shutdown()
}
