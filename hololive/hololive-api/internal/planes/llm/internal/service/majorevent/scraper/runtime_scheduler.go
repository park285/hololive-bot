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

package scraper

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/pkg/httputil"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/majorevent"
)

// RuntimeScheduler는 RSS 수집/유지보수 스케줄러를 통합 실행한다.
type RuntimeScheduler struct {
	feedScheduler        *FeedScheduler
	maintenanceScheduler *MaintenanceScheduler
	logger               *slog.Logger
}

// NewRuntimeScheduler는 RuntimeScheduler를 생성한다.
func NewRuntimeScheduler(repository *majorevent.Repository, logger *slog.Logger) (*RuntimeScheduler, error) {
	if repository == nil {
		return nil, fmt.Errorf("new major event scraper runtime scheduler: repository is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	httpClient := httputil.NewExternalAPIClient(defaultFeedHTTPTimeout)

	fetcher := NewFeedFetcher(httpClient, DefaultFeedFetcherConfig())
	parser := NewRSSParser()
	service, err := NewService(repository, fetcher, parser, DefaultServiceConfig(), logger)
	if err != nil {
		return nil, fmt.Errorf("new major event scraper runtime scheduler: new service: %w", err)
	}

	feedScheduler, err := NewFeedScheduler(service, DefaultFeedScheduleConfig(), logger)
	if err != nil {
		return nil, fmt.Errorf("new major event scraper runtime scheduler: new feed scheduler: %w", err)
	}

	linkChecker := NewLinkChecker(httpClient, DefaultLinkCheckerConfig(), logger)
	maintenanceScheduler, err := NewMaintenanceScheduler(repository, linkChecker, DefaultMaintenanceConfig(), logger)
	if err != nil {
		return nil, fmt.Errorf("new major event scraper runtime scheduler: new maintenance scheduler: %w", err)
	}

	return &RuntimeScheduler{
		feedScheduler:        feedScheduler,
		maintenanceScheduler: maintenanceScheduler,
		logger:               logger,
	}, nil
}

// Start는 통합 스케줄러를 시작한다.
func (r *RuntimeScheduler) Start(ctx context.Context) {
	if r == nil {
		return
	}
	if r.feedScheduler != nil {
		r.feedScheduler.Start(ctx)
	}
	if r.maintenanceScheduler != nil {
		r.maintenanceScheduler.Start(ctx)
	}
}

// Stop은 통합 스케줄러를 종료한다.
func (r *RuntimeScheduler) Stop() {
	if r == nil {
		return
	}
	if r.feedScheduler != nil {
		r.feedScheduler.Stop()
	}
	if r.maintenanceScheduler != nil {
		r.maintenanceScheduler.Stop()
	}
	if r.logger != nil {
		r.logger.Info("Major event scraper runtime scheduler stopped")
	}
}
