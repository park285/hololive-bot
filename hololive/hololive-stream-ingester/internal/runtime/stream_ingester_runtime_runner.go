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

package runtime

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

// StreamIngesterRuntime: stream-ingester 전용 런타임 (YouTube/스크래퍼/PhotoSync/Outbox).
type StreamIngesterRuntime struct {
	RuntimeName string
	Config      *config.Config
	Logger      *slog.Logger

	Scheduler           youtube.Scheduler
	ScraperScheduler    *poller.Scheduler
	PublishedAtResolver *poller.PendingPublishedAtResolver
	PhotoSync           *holodex.PhotoSyncService
	OutboxDispatcher    *outbox.Dispatcher
	ConfigSubscriber    *configsub.Subscriber
	PollTargetRefresher *youTubePollTargetRefresher

	ServerAddr string
	HttpServer *http.Server

	Readiness *ingestionReadinessState

	CommunityShortsBigBangPolicy           communityShortsBigBangPolicy
	communityShortsObservationWindowWriter communityShortsObservationWindowWriter
	timeNow                                func() time.Time

	ingestionLease *providers.IngestionLease
	lifecycle.Managed
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
	_ = lifecycle.Run(lifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start: func(ctx context.Context, errCh chan<- error) {
			r.startBackgroundServices(ctx, errCh)
			r.startHTTPServer(errCh)
			if err := r.ensureCommunityShortsObservationWindow(ctx); err != nil {
				errCh <- err
				return
			}
			if r.Readiness != nil {
				r.Readiness.markRunning()
			}
		},
		OnSignal: func(sig os.Signal) {
			if r.Readiness != nil {
				r.Readiness.markStopping("")
			}
			r.Logger.Info("Received shutdown signal",
				slog.String("runtime", r.runtimeName()),
				slog.String("signal", sig.String()),
			)
		},
		OnError: func(err error) {
			if r.Readiness != nil {
				r.Readiness.markStopping(err.Error())
			}
			r.Logger.Error("Server error",
				slog.String("runtime", r.runtimeName()),
				slog.Any("error", err),
			)
		},
		Shutdown: func(ctx context.Context) error {
			r.shutdown(ctx)
			return nil
		},
	})
}
