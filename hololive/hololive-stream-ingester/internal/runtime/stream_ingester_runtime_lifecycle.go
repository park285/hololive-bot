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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

func (r *StreamIngesterRuntime) startBackgroundServices(ctx context.Context, errCh chan<- error) {
	if r.ConfigSubscriber != nil {
		go r.ConfigSubscriber.Run(ctx)
		r.Logger.Info("Config subscriber started", slog.String("runtime", r.runtimeName()))
	}
	if r.ingestionLease != nil {
		go r.ingestionLease.StartRenewLoop(ctx, errCh)
	}
	if r.Scheduler != nil {
		r.Scheduler.Start(ctx)
		r.Logger.Info("YouTube ingestion scheduler started", slog.String("runtime", r.runtimeName()))
	}
	if r.ScraperScheduler != nil {
		r.ScraperScheduler.Start(ctx)
		r.Logger.Info("Scraper scheduler started", slog.String("runtime", r.runtimeName()))
	}
	if r.PollTargetRefresher != nil {
		go r.PollTargetRefresher.Start(ctx)
		r.Logger.Info("YouTube poll target refresher started", slog.String("runtime", r.runtimeName()))
	}
	if r.OutboxDispatcher != nil {
		r.OutboxDispatcher.Start(ctx)
		r.Logger.Info("YouTube outbox dispatcher started", slog.String("runtime", r.runtimeName()))
	}
	if r.PhotoSync != nil {
		go r.PhotoSync.Start(ctx)
		r.Logger.Info("Photo sync service started", slog.String("runtime", r.runtimeName()))
	}
}

func (r *StreamIngesterRuntime) startHTTPServer(errCh chan<- error) {
	go func() {
		if err := r.HttpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server error: %w", err)
		}
	}()
	r.Logger.Info("Ingestion runtime HTTP server started",
		slog.String("runtime", r.runtimeName()),
		slog.String("addr", r.ServerAddr),
	)
}

func (r *StreamIngesterRuntime) shutdown(ctx context.Context) {
	if r.Readiness != nil {
		r.Readiness.markStopping("")
	}

	if r.Scheduler != nil {
		r.Scheduler.Stop()
	}
	if r.ScraperScheduler != nil {
		r.ScraperScheduler.Stop()
	}
	if r.HttpServer != nil {
		if err := r.HttpServer.Shutdown(ctx); err != nil {
			r.Logger.Error("Ingestion runtime HTTP shutdown failed",
				slog.String("runtime", r.runtimeName()),
				slog.Any("error", err),
			)
		}
	}
	if r.ingestionLease != nil {
		if err := r.ingestionLease.Release(ctx); err != nil {
			r.Logger.Error("Ingestion runtime lease release failed",
				slog.String("runtime", r.runtimeName()),
				slog.Any("error", err),
			)
		}
	}
}
