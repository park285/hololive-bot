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

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
)

// BuildStreamIngesterRuntime: stream-ingester 런타임을 구성합니다.
func BuildStreamIngesterRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*StreamIngesterRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}

	logger.Info("Stream-ingester ingestion runtime enabled",
		slog.String("event", "stream_ingestion_enabled"),
		slog.String("lock_key", providers.IngestionLeaseKey),
	)

	infra, err := initStreamIngesterInfrastructure(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	ingestionLeaseRef, err := providers.AcquireIngestionLease(ctx, infra.cacheService, "stream-ingester", logger)
	if err != nil {
		infra.cleanupDB()
		infra.cleanupCache()
		return nil, fmt.Errorf("acquire ingestion lease: %w", err)
	}

	scraperScheduler, outboxDispatcher := buildStreamIngesterYouTubeComponents(
		cfg.Scraper,
		infra.postgresService,
		infra.membersData,
		infra.cacheService,
		infra.irisClient,
		infra.templateRenderer,
		infra.sharedRL,
		logger,
	)
	youtubeScheduler := infra.ytStack.Scheduler
	configSubscriber := buildStreamIngesterConfigSubscriber(
		infra.cacheService,
		infra.settingsService,
		infra.holodexService,
		infra.ytStack,
		scraperScheduler,
		logger,
	)

	desiredProxyState := infra.settingsService.Get().ScraperProxyEnabled
	applyScraperProxyToggle(
		desiredProxyState,
		ProvideYouTubeService(infra.ytStack),
		infra.holodexService,
		scraperScheduler,
		logger,
	)

	httpServer, err := buildStreamIngesterHTTPServer(ctx, cfg, logger)
	if err != nil {
		infra.cleanupDB()
		infra.cleanupCache()
		return nil, err
	}

	cleanup := func() {
		infra.cleanupDB()
		infra.cleanupCache()
	}

	return &StreamIngesterRuntime{
		Config:           cfg,
		Logger:           logger,
		Scheduler:        youtubeScheduler,
		ScraperScheduler: scraperScheduler,
		PhotoSync:        infra.photoSync,
		OutboxDispatcher: outboxDispatcher,
		ConfigSubscriber: configSubscriber,
		ServerAddr:       ProvideAPIAddr(cfg),
		HttpServer:       httpServer,
		ingestionLease:   ingestionLeaseRef,
		cleanup:          cleanup,
	}, nil
}

func buildStreamIngesterHTTPServer(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*http.Server, error) {
	router, err := ProvideHealthOnlyRouter(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("build stream ingester router: %w", err)
	}
	return ProvideAPIServer(ProvideAPIAddr(cfg), router), nil
}
