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
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicguard"
)

type maintenanceRepository interface {
	UpdateExpiredEvents(ctx context.Context) (int64, error)
	GetAllActiveEvents(ctx context.Context) ([]*domain.MajorEvent, error)
}

// MaintenanceScheduler는 만료 상태 업데이트와 링크 검증을 주기적으로 수행한다.
type MaintenanceScheduler struct {
	repository  maintenanceRepository
	linkChecker *LinkChecker
	config      MaintenanceConfig
	logger      *slog.Logger
	nowFn       func() time.Time

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

type maintenanceTrigger int

const (
	maintenanceTriggerContextDone maintenanceTrigger = iota
	maintenanceTriggerStop
	maintenanceTriggerExpired
	maintenanceTriggerLinkCheck
)

// NewMaintenanceScheduler는 MaintenanceScheduler를 생성한다.
func NewMaintenanceScheduler(
	repository maintenanceRepository,
	linkChecker *LinkChecker,
	config MaintenanceConfig,
	logger *slog.Logger,
) (*MaintenanceScheduler, error) {
	if repository == nil {
		return nil, fmt.Errorf("new maintenance scheduler: repository is nil")
	}
	if linkChecker == nil {
		return nil, fmt.Errorf("new maintenance scheduler: link checker is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	normalized := config
	defaults := DefaultMaintenanceConfig()
	if normalized.RunTimeout <= 0 {
		normalized.RunTimeout = defaults.RunTimeout
	}
	if normalized.LinkCheckInterval <= 0 {
		normalized.LinkCheckInterval = defaults.LinkCheckInterval
	}

	return &MaintenanceScheduler{
		repository:  repository,
		linkChecker: linkChecker,
		config:      normalized,
		logger:      logger,
		nowFn:       time.Now,
		stopCh:      make(chan struct{}),
	}, nil
}

// Start는 유지보수 스케줄러를 시작한다.
func (s *MaintenanceScheduler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.wg.Add(1)
	panicguard.Go(s.logger, "major-event-maintenance-scheduler", func() {
		s.run(ctx)
	})
}

// Stop은 유지보수 스케줄러를 종료한다.
func (s *MaintenanceScheduler) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *MaintenanceScheduler) run(ctx context.Context) {
	defer s.wg.Done()

	linkTicker := time.NewTicker(s.config.LinkCheckInterval)
	defer linkTicker.Stop()

	for {
		if !s.runMaintenanceCycle(ctx, linkTicker.C) {
			return
		}
	}
}

func (s *MaintenanceScheduler) runMaintenanceCycle(ctx context.Context, linkTicker <-chan time.Time) bool {
	nextExpiredRun := calculateNextRunAtHour(s.now(), s.config.ExpireHourKST)
	waitDuration := max(time.Until(nextExpiredRun), time.Duration(0))

	s.logger.Info(
		"Major event maintenance scheduler waiting",
		slog.String("next_expired_run_kst", formatKST(nextExpiredRun)),
		slog.Duration("wait_duration", waitDuration),
	)

	expiredTimer := time.NewTimer(waitDuration)
	trigger := waitMaintenanceTrigger(ctx, s.stopCh, expiredTimer.C, linkTicker)
	if trigger == maintenanceTriggerContextDone {
		expiredTimer.Stop()
		s.logger.Info("Major event maintenance scheduler stopped by context")
		return false
	}
	if trigger == maintenanceTriggerStop {
		expiredTimer.Stop()
		s.logger.Info("Major event maintenance scheduler stopped")
		return false
	}
	if trigger == maintenanceTriggerExpired {
		s.runExpiredUpdate(ctx)
	}
	if trigger == maintenanceTriggerLinkCheck {
		expiredTimer.Stop()
		s.runLinkCheck(ctx)
	}
	return true
}

func waitMaintenanceTrigger(
	ctx context.Context,
	stopCh <-chan struct{},
	expiredTimer <-chan time.Time,
	linkTicker <-chan time.Time,
) maintenanceTrigger {
	select {
	case <-ctx.Done():
		return maintenanceTriggerContextDone
	case <-stopCh:
		return maintenanceTriggerStop
	case <-expiredTimer:
		return maintenanceTriggerExpired
	case <-linkTicker:
		return maintenanceTriggerLinkCheck
	}
}

func (s *MaintenanceScheduler) runExpiredUpdate(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, s.config.RunTimeout)
	defer cancel()

	updated, err := s.repository.UpdateExpiredEvents(runCtx)
	if err != nil {
		s.logger.Warn("Major event expired update failed", slog.String("error", err.Error()))
		return
	}
	if updated > 0 {
		s.logger.Info("Major event expired rows updated", slog.Int64("updated_rows", updated))
	}
}

func (s *MaintenanceScheduler) runLinkCheck(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, s.config.RunTimeout)
	defer cancel()

	events, err := s.repository.GetAllActiveEvents(runCtx)
	if err != nil {
		s.logger.Warn("Major event active event load failed for link check", slog.String("error", err.Error()))
		return
	}
	if len(events) == 0 {
		return
	}

	result, err := s.linkChecker.CheckEvents(runCtx, events)
	if err != nil {
		s.logger.Warn("Major event link check failed", slog.String("error", err.Error()))
		return
	}

	s.logger.Info(
		"Major event link check completed",
		slog.Int("checked", result.Checked),
		slog.Int("ok", result.OK),
		slog.Int("failed", result.Failed),
		slog.Int("blocked", result.Blocked),
	)
}

func (s *MaintenanceScheduler) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn().UTC()
	}
	return time.Now().UTC()
}
