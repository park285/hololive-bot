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
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

const (
	defaultYouTubeInterval = 60 * time.Second
	defaultLiveInterval    = 120 * time.Second

	defaultYouTubeTimeout  = 45 * time.Second
	defaultPlatformTimeout = 30 * time.Second
	evaluationWindowSlack  = 15 * time.Second
)

type targetMinutesSource interface {
	GetTargetMinutes() []int
}

type targetMinutesUpdater interface {
	UpdateTargetMinutes([]int)
}

// RuntimeScheduler는 런타임 알람 체크 루프를 관리한다.
type RuntimeScheduler struct {
	youtubeChecker checker.Runner
	chzzkChecker   checker.Runner
	twitchChecker  checker.Runner
	notifier       checker.Sender
	cacheSvc       cache.Client

	youtubeTargetUpdater  targetMinutesUpdater
	dedupTargetUpdater    targetMinutesUpdater
	targetMinutesSource   targetMinutesSource
	alarmCacheWarmer      alarmCacheWarmer
	platformMappingSyncer alarmPlatformMappingSyncer

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
	alarmCRUD domain.AlarmCRUD,
	notifCfg config.NotificationConfig,
	outbox dispatchoutbox.Writer,
	publishCfg queue.PublishConfig,
	twitchEnabled bool,
	logger *slog.Logger,
) (*RuntimeScheduler, error) {
	if err := validateRuntimeSchedulerDeps(cacheSvc, holodexSvc, chzzkClient, twitchClient, alarmCRUD); err != nil {
		return nil, err
	}

	logger = runtimeSchedulerLogger(logger)

	targetMinutes := sharedchecker.NormalizeTargetMinutes(alarmCRUD.GetTargetMinutes())
	youtubeInterval, youtubeEvaluationWindowCap := runtimeSchedulerYouTubeTiming(notifCfg.CheckInterval)
	tierScheduler := tier.NewTieredScheduler(logger)
	dedupSvc := dedup.NewService(cacheSvc, targetMinutes, logger)
	queuePublisher := newRuntimeSchedulerQueuePublisher(cacheSvc, logger, outbox, publishCfg)

	youtubeChecker, err := newRuntimeSchedulerYouTubeChecker(
		cacheSvc,
		holodexSvc,
		tierScheduler,
		dedupSvc,
		targetMinutes,
		youtubeEvaluationWindowCap,
		logger,
	)
	if err != nil {
		return nil, err
	}

	chzzkChecker, err := checker.NewChzzkChecker(cacheSvc, chzzkClient, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create chzzk checker: %w", err)
	}

	twitchChecker, err := newOptionalTwitchChecker(cacheSvc, twitchClient, twitchEnabled, logger)
	if err != nil {
		return nil, err
	}
	notifierSvc, err := checker.NewNotifier(dedupSvc, queuePublisher, tierScheduler, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create notifier: %w", err)
	}

	return newRuntimeSchedulerInstance(cacheSvc, alarmCRUD, youtubeChecker, chzzkChecker, twitchChecker, notifierSvc, dedupSvc, youtubeInterval, logger), nil
}

func newRuntimeSchedulerYouTubeChecker(
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	tierScheduler *tier.TieredScheduler,
	dedupSvc *dedup.Service,
	targetMinutes []int,
	evaluationWindowCap time.Duration,
	logger *slog.Logger,
) (*checker.YouTubeChecker, error) {
	youtubeChecker, err := checker.NewYouTubeChecker(
		cacheSvc,
		holodexSvc,
		tierScheduler,
		dedupSvc,
		targetMinutes,
		evaluationWindowCap,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create youtube checker: %w", err)
	}

	return youtubeChecker, nil
}

func runtimeSchedulerLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}

	return logger
}

func validateRuntimeSchedulerDeps(
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	alarmCRUD domain.AlarmCRUD,
) error {
	if cacheSvc == nil {
		return errors.New("new runtime scheduler: cache service is nil")
	}

	if holodexSvc == nil {
		return errors.New("new runtime scheduler: holodex service is nil")
	}

	if chzzkClient == nil {
		return errors.New("new runtime scheduler: chzzk client is nil")
	}

	if twitchClient == nil {
		return errors.New("new runtime scheduler: twitch client is nil")
	}

	if alarmCRUD == nil {
		return errors.New("new runtime scheduler: alarm CRUD is nil")
	}

	return nil
}

func runtimeSchedulerYouTubeTiming(checkInterval time.Duration) (time.Duration, time.Duration) {
	evaluationWindowCap := youtubeEvaluationWindowCap(checkInterval)
	if checkInterval <= 0 {
		return defaultYouTubeInterval, evaluationWindowCap
	}

	return checkInterval, evaluationWindowCap
}

func newRuntimeSchedulerQueuePublisher(
	cacheSvc cache.Client,
	logger *slog.Logger,
	outbox dispatchoutbox.Writer,
	publishCfg queue.PublishConfig,
) *queue.Publisher {
	return queue.NewPublisher(
		cacheSvc,
		logger,
		queue.WithOutbox(outbox),
		queue.WithPublishMode(publishCfg.Mode),
		queue.WithShadowFatal(publishCfg.ShadowFatal),
		queue.WithWakeupEnabled(publishCfg.WakeupEnabled),
		queue.WithMaxDeliveriesPerBatch(publishCfg.MaxDeliveriesPerBatch),
	)
}

func newRuntimeSchedulerInstance(
	cacheSvc cache.Client,
	alarmCRUD domain.AlarmCRUD,
	youtubeChecker *checker.YouTubeChecker,
	chzzkChecker checker.Runner,
	twitchChecker checker.Runner,
	notifierSvc checker.Sender,
	dedupSvc targetMinutesUpdater,
	youtubeInterval time.Duration,
	logger *slog.Logger,
) *RuntimeScheduler {
	return &RuntimeScheduler{
		youtubeChecker: youtubeChecker,
		chzzkChecker:   chzzkChecker,
		twitchChecker:  twitchChecker,
		notifier:       notifierSvc,
		cacheSvc:       cacheSvc,

		youtubeTargetUpdater:  youtubeChecker,
		dedupTargetUpdater:    dedupSvc,
		targetMinutesSource:   alarmCRUD,
		alarmCacheWarmer:      alarmCRUD,
		platformMappingSyncer: alarmPlatformMappingSyncerFrom(alarmCRUD),

		youtubeInterval: youtubeInterval,
		chzzkInterval:   defaultLiveInterval,
		twitchInterval:  defaultLiveInterval,

		youtubeTimeout: defaultYouTubeTimeout,
		chzzkTimeout:   defaultPlatformTimeout,
		twitchTimeout:  defaultPlatformTimeout,

		logger: logger,
	}
}

// Start는 3개 플랫폼 루프를 병렬 실행하고 context 취소 시 종료한다.
func (s *RuntimeScheduler) Start(ctx context.Context) error {
	if s == nil {
		return errors.New("runtime scheduler is nil")
	}
	if ctx == nil {
		return errors.New("runtime scheduler context is nil")
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return s.runLoop(egCtx, "youtube", s.youtubeInterval, s.youtubeTimeout, false, s.runYouTubeIteration)
	})
	eg.Go(func() error {
		return s.runLoop(egCtx, "chzzk", s.chzzkInterval, s.chzzkTimeout, true, s.runChzzkIteration)
	})
	s.startTwitchLoop(eg, egCtx)
	eg.Go(func() error {
		return s.runAlarmCacheRecoveryLoop(egCtx)
	})

	if err := eg.Wait(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}

		return fmt.Errorf("runtime scheduler stopped: %w", err)
	}

	return nil
}

const runtimeSchedulerLoopNameTwitch = "twitch"

func (s *RuntimeScheduler) runLoop(
	ctx context.Context,
	name string,
	interval time.Duration,
	timeout time.Duration,
	runImmediately bool,
	run func(context.Context) error,
) error {
	next := time.NewTimer(initialLoopDelay(time.Now(), interval, runImmediately))
	defer next.Stop()

	for {
		if err := s.waitForLoopTick(ctx, name, next); err != nil {
			return err
		}
		s.runLoopIteration(ctx, name, timeout, run)
		next.Reset(nextLoopDelay(time.Now(), interval))
	}
}

func (s *RuntimeScheduler) waitForLoopTick(ctx context.Context, name string, next *time.Timer) error {
	select {
	case <-ctx.Done():
		s.logger.Info("Alarm loop stopped", slog.String("loop", name))
		return ctx.Err()
	case <-next.C:
		return nil
	}
}

func (s *RuntimeScheduler) runLoopIteration(
	ctx context.Context,
	name string,
	timeout time.Duration,
	run func(context.Context) error,
) {
	loopCtx, cancel := context.WithTimeout(ctx, timeout)
	err := run(loopCtx)

	cancel()

	if err != nil {
		s.logger.Warn("Alarm loop iteration failed",
			slog.String("loop", name),
			slog.Any("error", err),
		)
	}
}

func nextLoopDelay(now time.Time, interval time.Duration) time.Duration {
	delay := time.Until(nextAligned(now, interval))
	if delay < 0 {
		return 0
	}

	return delay
}

func initialLoopDelay(now time.Time, interval time.Duration, runImmediately bool) time.Duration {
	if runImmediately || interval <= 0 {
		return 0
	}

	firstRunAt := firstAlignedRunAt(now, interval)
	if !firstRunAt.After(now) {
		return 0
	}

	return firstRunAt.Sub(now)
}

func firstAlignedRunAt(now time.Time, interval time.Duration) time.Time {
	if interval <= 0 {
		return now
	}

	if now.Equal(now.Truncate(interval)) {
		return now
	}

	return nextAligned(now, interval)
}

func nextAligned(now time.Time, interval time.Duration) time.Time {
	if interval <= 0 {
		return now
	}

	next := now.Truncate(interval).Add(interval)
	if next.After(now) {
		return next
	}

	return next.Add(interval)
}

func youtubeEvaluationWindowCap(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = defaultYouTubeInterval
	}
	if interval < time.Minute {
		return time.Minute + evaluationWindowSlack
	}

	return interval + evaluationWindowSlack
}

func (s *RuntimeScheduler) runYouTubeIteration(ctx context.Context) error {
	s.syncYouTubeTargetMinutes()

	notifications, err := s.youtubeChecker.Check(ctx)
	if err != nil {
		if recoveryErr := s.recoverAlarmCacheAfterCheckFailure(ctx, err); recoveryErr != nil {
			s.logger.Warn("Immediate alarm cache recovery failed after YouTube check error", slog.Any("error", recoveryErr))
		}
		return fmt.Errorf("run youtube iteration: check notifications: %w", err)
	}

	return s.dispatchNotifications(ctx, "youtube", notifications)
}

func (s *RuntimeScheduler) syncYouTubeTargetMinutes() {
	if s.targetMinutesSource == nil {
		return
	}

	targetMinutes := s.targetMinutesSource.GetTargetMinutes()
	if s.youtubeTargetUpdater != nil {
		s.youtubeTargetUpdater.UpdateTargetMinutes(targetMinutes)
	}
	if s.dedupTargetUpdater != nil {
		s.dedupTargetUpdater.UpdateTargetMinutes(targetMinutes)
	}
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

	s.logger.Debug("Alarm notifications dispatched",
		slog.String("loop", loopName),
		slog.Int("sent", sendResult.Sent),
		slog.Int("skipped", sendResult.Skipped),
		slog.Int("failed", sendResult.Failed),
	)

	if err != nil {
		return fmt.Errorf("dispatch notifications: send notifications partially failed: %w", err)
	}

	return nil
}
