package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedlog "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
	"golang.org/x/sync/errgroup"
)

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
	defer cancel()

	_ = sharedlog.RunOperation(loopCtx, s.logger, sharedlog.OperationOptions{
		Name:         "alarm.scheduler.loop.iteration",
		IDPrefix:     "alarm_" + name,
		Runtime:      "alarm-worker",
		Component:    "scheduler",
		StartEvent:   EventAlarmSchedulerLoopIterationStarted,
		SuccessEvent: EventAlarmSchedulerLoopIterationSucceeded,
		FailureEvent: EventAlarmSchedulerLoopIterationFailed,
		Attrs: []slog.Attr{
			slog.String("loop", name),
			slog.Duration("timeout", timeout),
		},
	}, run)
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

	if err != nil {
		attrs := []slog.Attr{
			slog.String("loop", loopName),
			slog.Int("notifications", len(notifications)),
			slog.Int("sent", sendResult.Sent),
			slog.Int("skipped", sendResult.Skipped),
			slog.Int("failed", sendResult.Failed),
		}
		attrs = append(attrs, sharedlog.ErrorAttrs(err)...)
		sharedlog.Warn(ctx, s.logger, EventAlarmNotificationDispatchFailed, "alarm notification dispatch failed", attrs...)
		return fmt.Errorf("dispatch notifications: send notifications partially failed: %w", err)
	}

	sharedlog.Info(ctx, s.logger, EventAlarmNotificationDispatchSucceeded, "alarm notifications dispatched",
		slog.String("loop", loopName),
		slog.Int("notifications", len(notifications)),
		slog.Int("sent", sendResult.Sent),
		slog.Int("skipped", sendResult.Skipped),
		slog.Int("failed", sendResult.Failed),
	)

	return nil
}
