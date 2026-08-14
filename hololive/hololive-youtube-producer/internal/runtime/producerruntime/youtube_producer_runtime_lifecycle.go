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
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/panicguard"
	sharedlog "github.com/park285/shared-go/pkg/logging"
)

func (r *YouTubeProducerRuntime) startBackgroundServices(ctx context.Context, errCh chan<- error) {
	prefix := r.runtimeName()
	if r.ConfigSubscriber != nil {
		r.startBackgroundService(prefix+"-config-subscriber", func() {
			r.ConfigSubscriber.Run(ctx)
		})
		r.Logger.Info("Config subscriber started", slog.String("runtime", r.runtimeName()))
	}
	if r.ingestionLease != nil {
		r.startBackgroundService(prefix+"-ingestion-lease", func() {
			r.ingestionLease.StartRenewLoop(ctx, errCh)
		})
	}
	if r.ScraperScheduler != nil {
		r.ScraperScheduler.Start(ctx)
		r.Logger.Info("Scraper scheduler started", slog.String("runtime", r.runtimeName()))
	}
	if r.runActiveActiveRecovery != nil {
		r.startBackgroundService(prefix+"-readiness-recovery-owner", func() {
			r.runActiveActiveRecovery(ctx)
		})
	}
	if r.PollTargetRefresher != nil {
		r.startBackgroundService(prefix+"-poll-target-refresh", func() {
			r.PollTargetRefresher.Start(ctx)
		})
		r.Logger.Info("YouTube poll target refresher started", slog.String("runtime", r.runtimeName()))
	}
	if r.PhotoSync != nil {
		r.startBackgroundService(prefix+"-photo-sync", func() {
			r.PhotoSync.Start(ctx)
		})
		r.Logger.Info("Photo sync service started", slog.String("runtime", r.runtimeName()))
	}
	if r.RetentionCleaner != nil {
		r.startBackgroundService(prefix+"-retention-cleaner", func() {
			r.RetentionCleaner.Start(ctx)
		})
		r.Logger.Info("YouTube retention cleaner started", slog.String("runtime", r.runtimeName()))
	}
}

func (r *YouTubeProducerRuntime) startBackgroundService(name string, run func()) {
	r.backgroundWG.Add(1)
	r.activeBackgrounds.Add(1)
	panicguard.Go(r.Logger, name, func() {
		defer r.backgroundWG.Done()
		defer r.activeBackgrounds.Add(-1)
		run()
	})
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

func (r *YouTubeProducerRuntime) shutdown(ctx context.Context) error {
	if r.Readiness != nil {
		r.Readiness.MarkStopping("")
	}

	r.stopSchedulers()
	r.shutdownHTTPServer(ctx)
	if err := r.waitForBackgroundServices(ctx); err != nil {
		return err
	}
	r.releaseIngestionLease(ctx)
	sharedlog.Info(ctx, r.Logger, EventIngestionRuntimeStopped, "ingestion runtime stopped",
		sharedlog.Runtime(r.runtimeName()),
	)
	return nil
}

func (r *YouTubeProducerRuntime) waitForBackgroundServices(ctx context.Context) error {
	if r.activeBackgrounds.Load() == 0 {
		return nil
	}
	done := make(chan struct{})
	panicguard.Go(r.Logger, "youtube-producer-background-join", func() {
		r.backgroundWG.Wait()
		close(done)
	})
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for runtime background services: %w", ctx.Err())
	}
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
