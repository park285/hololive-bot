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

package producerruntime

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/kapu/hololive-shared/pkg/constants"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/configsub"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/retention"
	sharedlog "github.com/park285/shared-go/pkg/logging"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

type YouTubeProducerRuntime struct {
	RuntimeName string
	Config      *settings.Config
	Logger      *slog.Logger

	ScraperScheduler    *scheduler.Scheduler
	PhotoSync           photoSyncService
	ConfigSubscriber    *configsub.Subscriber
	PollTargetRefresher *polltarget.Refresher
	RetentionCleaner    *retention.Cleaner

	ServerAddr  string
	HTTPServers *sharedserver.RuntimeHTTPServers

	Readiness *readiness.State

	ingestionLease          *ingestionlease.Lease
	runActiveActiveRecovery func(context.Context)
	backgroundWG            sync.WaitGroup
	activeBackgrounds       atomic.Int64
	lifecycle.Managed
}

func (r *YouTubeProducerRuntime) runtimeName() string {
	if r == nil {
		return youtubeProducerRuntimeName
	}
	name := strings.TrimSpace(r.RuntimeName)
	if name == "" {
		return youtubeProducerRuntimeName
	}
	return name
}

func (r *YouTubeProducerRuntime) Close() {
	if r == nil {
		return
	}
	if active := r.activeBackgrounds.Load(); active != 0 {
		if r.Logger != nil {
			r.Logger.Error("Runtime dependency cleanup skipped while background services are active",
				slog.Int64("active_background_services", active),
			)
		}
		return
	}
	r.Managed.Close()
}

func (r *YouTubeProducerRuntime) Run() error {
	var runtimeErr error
	err := lifecycle.Run(lifecycle.Options{
		ShutdownTimeout: constants.AppTimeout.Shutdown,
		Start:           r.startRuntime,
		OnSignal:        r.handleShutdownSignal,
		OnError: func(err error) {
			runtimeErr = err
			r.handleRuntimeError(err)
		},
		Shutdown: r.shutdownRuntime,
	})
	if runtimeErr != nil && !errors.Is(err, runtimeErr) {
		err = errors.Join(runtimeErr, err)
	}
	if err != nil && runtimeErr == nil {
		r.handleRuntimeError(err)
	}
	return err
}

func (r *YouTubeProducerRuntime) startRuntime(ctx context.Context, errCh chan<- error) {
	r.startBackgroundServices(ctx, errCh)
	r.startHTTPServer(errCh)
	if r.Readiness != nil {
		r.Readiness.MarkRunning()
	}
	sharedlog.Info(ctx, r.Logger, EventIngestionRuntimeStarted, "ingestion runtime started",
		sharedlog.Runtime(r.runtimeName()),
		slog.Bool("youtube_scheduler_enabled", r.ScraperScheduler != nil),
		slog.Bool("photo_sync_enabled", r.PhotoSync != nil),
	)
}

func (r *YouTubeProducerRuntime) handleShutdownSignal(sig os.Signal) {
	if r.Readiness != nil {
		r.Readiness.MarkStopping("")
	}
	r.Logger.Info("Received shutdown signal",
		slog.String("runtime", r.runtimeName()),
		slog.String("signal", sig.String()),
	)
}

func (r *YouTubeProducerRuntime) handleRuntimeError(err error) {
	if r.Readiness != nil {
		r.Readiness.MarkStopping(err.Error())
	}
	r.Logger.Error("Server error",
		slog.String("runtime", r.runtimeName()),
		slog.Any("error", err),
	)
}

func (r *YouTubeProducerRuntime) shutdownRuntime(ctx context.Context) error {
	return r.shutdown(ctx)
}

func buildRetentionCleaner(infra *youtubeProducerInfrastructure, logger *slog.Logger) *retention.Cleaner {
	if infra == nil || infra.postgresService == nil {
		return nil
	}
	pool := infra.postgresService.GetPool()
	if pool == nil {
		return nil
	}
	cfg := retention.LoadConfig()
	if !cfg.Enabled() {
		return nil
	}
	return retention.NewCleaner(pool, cfg, logger)
}
