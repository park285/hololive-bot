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

package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/alarm/checker"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

const (
	defaultYouTubeInterval = 60 * time.Second
	defaultLiveInterval    = 120 * time.Second

	defaultYouTubeTimeout  = 45 * time.Second
	defaultPlatformTimeout = 30 * time.Second
)

// RuntimeScheduler는 런타임 알람 체크 루프를 관리한다.
type RuntimeScheduler struct {
	youtubeChecker checker.Runner
	chzzkChecker   checker.Runner
	twitchChecker  checker.Runner
	notifier       checker.Sender

	youtubeInterval time.Duration
	chzzkInterval   time.Duration
	twitchInterval  time.Duration

	youtubeTimeout time.Duration
	chzzkTimeout   time.Duration
	twitchTimeout  time.Duration

	logger *slog.Logger
}

// NewRuntimeScheduler는 런타임 알람 스케줄러를 생성한다.
func NewRuntimeScheduler(
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	alarmSvc *notification.AlarmService,
	notifCfg config.NotificationConfig,
	logger *slog.Logger,
) (*RuntimeScheduler, error) {
	if cacheSvc == nil {
		return nil, fmt.Errorf("new runtime scheduler: cache service is nil")
	}
	if holodexSvc == nil {
		return nil, fmt.Errorf("new runtime scheduler: holodex service is nil")
	}
	if chzzkClient == nil {
		return nil, fmt.Errorf("new runtime scheduler: chzzk client is nil")
	}
	if twitchClient == nil {
		return nil, fmt.Errorf("new runtime scheduler: twitch client is nil")
	}
	if alarmSvc == nil {
		return nil, fmt.Errorf("new runtime scheduler: alarm service is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	targetMinutes := normalizeTargetMinutes(notifCfg.AdvanceMinutes)
	if len(targetMinutes) == 0 {
		targetMinutes = normalizeTargetMinutes(alarmSvc.GetTargetMinutes())
	}

	tierScheduler := tier.NewTieredScheduler(logger)
	dedupSvc := dedup.NewService(cacheSvc, targetMinutes, logger)
	queuePublisher := queue.NewPublisher(cacheSvc, logger)

	youtubeChecker, err := checker.NewYouTubeChecker(cacheSvc, holodexSvc, tierScheduler, dedupSvc, targetMinutes, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create youtube checker: %w", err)
	}
	chzzkChecker, err := checker.NewChzzkChecker(cacheSvc, chzzkClient, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create chzzk checker: %w", err)
	}
	twitchChecker, err := checker.NewTwitchChecker(cacheSvc, twitchClient, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create twitch checker: %w", err)
	}
	notifierSvc, err := checker.NewNotifier(dedupSvc, queuePublisher, alarmSvc, tierScheduler, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create notifier: %w", err)
	}

	youtubeInterval := notifCfg.CheckInterval
	if youtubeInterval <= 0 {
		youtubeInterval = defaultYouTubeInterval
	}

	return &RuntimeScheduler{
		youtubeChecker: youtubeChecker,
		chzzkChecker:   chzzkChecker,
		twitchChecker:  twitchChecker,
		notifier:       notifierSvc,

		youtubeInterval: youtubeInterval,
		chzzkInterval:   defaultLiveInterval,
		twitchInterval:  defaultLiveInterval,

		youtubeTimeout: defaultYouTubeTimeout,
		chzzkTimeout:   defaultPlatformTimeout,
		twitchTimeout:  defaultPlatformTimeout,

		logger: logger,
	}, nil
}

// Start는 3개 플랫폼 루프를 병렬 실행하고 context 취소 시 종료한다.
func (s *RuntimeScheduler) Start(ctx context.Context) {
	if ctx == nil {
		s.logger.Warn("Runtime scheduler start skipped: nil context")
		return
	}

	var eg errgroup.Group
	eg.Go(func() error {
		s.runLoop(ctx, "youtube", s.youtubeInterval, s.youtubeTimeout, s.runYouTubeIteration)
		return nil
	})
	eg.Go(func() error {
		s.runLoop(ctx, "chzzk", s.chzzkInterval, s.chzzkTimeout, s.runChzzkIteration)
		return nil
	})
	eg.Go(func() error {
		s.runLoop(ctx, "twitch", s.twitchInterval, s.twitchTimeout, s.runTwitchIteration)
		return nil
	})

	_ = eg.Wait()
}

func (s *RuntimeScheduler) runLoop(
	ctx context.Context,
	name string,
	interval time.Duration,
	timeout time.Duration,
	run func(context.Context) error,
) {
	next := time.NewTimer(0)
	defer next.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Alarm loop stopped", slog.String("loop", name))
			return
		case <-next.C:
			loopCtx, cancel := context.WithTimeout(ctx, timeout)
			err := run(loopCtx)
			cancel()
			if err != nil {
				s.logger.Warn("Alarm loop iteration failed",
					slog.String("loop", name),
					slog.Any("error", err),
				)
			}
			next.Reset(interval)
		}
	}
}

func (s *RuntimeScheduler) runYouTubeIteration(ctx context.Context) error {
	notifications, err := s.youtubeChecker.Check(ctx)
	if err != nil {
		return fmt.Errorf("run youtube iteration: check notifications: %w", err)
	}
	return s.dispatchNotifications(ctx, "youtube", notifications)
}

func (s *RuntimeScheduler) runChzzkIteration(ctx context.Context) error {
	notifications, err := s.chzzkChecker.Check(ctx)
	if err != nil {
		return fmt.Errorf("run chzzk iteration: check notifications: %w", err)
	}
	return s.dispatchNotifications(ctx, "chzzk", notifications)
}

func (s *RuntimeScheduler) runTwitchIteration(ctx context.Context) error {
	notifications, err := s.twitchChecker.Check(ctx)
	if err != nil {
		return fmt.Errorf("run twitch iteration: check notifications: %w", err)
	}
	return s.dispatchNotifications(ctx, "twitch", notifications)
}

func (s *RuntimeScheduler) dispatchNotifications(
	ctx context.Context,
	loopName string,
	notifications []*domain.AlarmNotification,
) error {
	if len(notifications) == 0 {
		return nil
	}

	sendResult, err := s.notifier.Send(ctx, notifications)
	if err != nil {
		return fmt.Errorf("dispatch notifications: send notifications: %w", err)
	}

	s.logger.Debug("Alarm notifications dispatched",
		slog.String("loop", loopName),
		slog.Int("sent", sendResult.Sent),
		slog.Int("skipped", sendResult.Skipped),
		slog.Int("failed", sendResult.Failed),
	)

	return nil
}

func normalizeTargetMinutes(targetMinutes []int) []int {
	normalized := make([]int, 0, len(targetMinutes))
	seen := make(map[int]struct{}, len(targetMinutes))
	for _, minute := range targetMinutes {
		if minute <= 0 {
			continue
		}
		if _, ok := seen[minute]; ok {
			continue
		}
		seen[minute] = struct{}{}
		normalized = append(normalized, minute)
	}

	if len(normalized) == 0 {
		return []int{5, 3, 1}
	}

	slices.SortFunc(normalized, func(a, b int) int { return b - a })
	if _, ok := seen[1]; !ok {
		normalized = append(normalized, 1)
	}

	return normalized
}
