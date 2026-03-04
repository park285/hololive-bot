package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/service/majorevent"
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

	httpClient := &http.Client{
		Timeout: 20 * time.Second,
	}

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
