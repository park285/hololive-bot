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
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/panicguard"
	sharedlog "github.com/park285/shared-go/pkg/logging"
)

func (r *YouTubeProducerRuntime) startBackgroundServices(ctx context.Context, errCh chan<- error) {
	if r.ConfigSubscriber != nil {
		panicguard.Go(r.Logger, "youtube-producer-config-subscriber", func() {
			r.ConfigSubscriber.Run(ctx)
		})
		r.Logger.Info("Config subscriber started", slog.String("runtime", r.runtimeName()))
	}
	if r.ingestionLease != nil {
		panicguard.Go(r.Logger, "youtube-producer-ingestion-lease", func() {
			r.ingestionLease.StartRenewLoop(ctx, errCh)
		})
	}
	if r.ScraperScheduler != nil {
		r.ScraperScheduler.Start(ctx)
		r.Logger.Info("Scraper scheduler started", slog.String("runtime", r.runtimeName()))
	}
	if r.PollTargetRefresher != nil {
		panicguard.Go(r.Logger, "youtube-producer-poll-target-refresh", func() {
			r.PollTargetRefresher.Start(ctx)
		})
		r.Logger.Info("YouTube poll target refresher started", slog.String("runtime", r.runtimeName()))
	}
	if r.PhotoSync != nil {
		panicguard.Go(r.Logger, "youtube-producer-photo-sync", func() {
			r.PhotoSync.Start(ctx)
		})
		r.Logger.Info("Photo sync service started", slog.String("runtime", r.runtimeName()))
	}
	if r.RetentionCleaner != nil {
		panicguard.Go(r.Logger, "youtube-producer-retention-cleaner", func() {
			r.RetentionCleaner.Start(ctx)
		})
		r.Logger.Info("YouTube retention cleaner started", slog.String("runtime", r.runtimeName()))
	}
}

func (r *YouTubeProducerRuntime) startHTTPServer(errCh chan<- error) {
	if r.HTTPServers == nil {
		return
	}
	r.HTTPServers.Start(r.Logger, errCh)
	r.Logger.Info("Ingestion runtime HTTP server started",
		slog.String("runtime", r.runtimeName()),
		slog.String("addr", r.HTTPServers.Addr()),
	)
}

func (r *YouTubeProducerRuntime) shutdown(ctx context.Context) {
	if r.Readiness != nil {
		r.Readiness.MarkStopping("")
	}

	r.stopSchedulers()
	r.shutdownHTTPServer(ctx)
	r.releaseIngestionLease(ctx)
	sharedlog.Info(ctx, r.Logger, EventIngestionRuntimeStopped, "ingestion runtime stopped",
		sharedlog.Runtime(r.runtimeName()),
	)
}

func (r *YouTubeProducerRuntime) stopSchedulers() {
	if r.ScraperScheduler != nil {
		r.ScraperScheduler.Stop()
	}
}

func (r *YouTubeProducerRuntime) shutdownHTTPServer(ctx context.Context) {
	if r.HTTPServers == nil {
		return
	}
	if err := r.HTTPServers.Shutdown(ctx); err != nil {
		r.Logger.Error("Ingestion runtime HTTP shutdown failed",
			slog.String("runtime", r.runtimeName()),
			slog.Any("error", err),
		)
	}
}

func (r *YouTubeProducerRuntime) releaseIngestionLease(ctx context.Context) {
	if r.ingestionLease != nil {
		if err := r.ingestionLease.Release(ctx); err != nil {
			r.Logger.Error("Ingestion runtime lease release failed",
				slog.String("runtime", r.runtimeName()),
				slog.Any("error", err),
			)
		}
	}
}
