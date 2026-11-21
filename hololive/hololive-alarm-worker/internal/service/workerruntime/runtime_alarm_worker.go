package workerruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type Scheduler interface {
	Start(ctx context.Context) error
}

type AlarmWorkerRuntime struct {
	lifecycle.Managed

	Config *workerconfig.RuntimeConfig
	Logger *slog.Logger

	Scheduler           Scheduler
	NotificationEgress  Scheduler
	CelebrationRunner   Scheduler
	XSpacesRunner       Scheduler
	EventGradeRunner    Scheduler
	ServerAddr          string
	HTTPServers         *sharedserver.RuntimeHTTPServers
	WorkerObservability Scheduler

	schedulerMu     sync.Mutex
	schedulerCancel context.CancelFunc
	schedulerDone   chan struct{}
}

type NamedScheduler struct {
	Name      string
	Scheduler Scheduler
}

type notificationEgressRunner struct {
	runners []NamedScheduler
	logger  *slog.Logger
}

type youtubeOutboxDispatcherRunner struct {
	dispatcher *youtubedispatch.Dispatcher
	logger     *slog.Logger
}

func NewNotificationEgressRunner(runners []NamedScheduler, logger *slog.Logger) Scheduler {
	return notificationEgressRunner{
		runners: runners,
		logger:  logger,
	}
}

func NewYouTubeOutboxDispatcherRunner(dispatcher *youtubedispatch.Dispatcher, logger *slog.Logger) Scheduler {
	return youtubeOutboxDispatcherRunner{dispatcher: dispatcher, logger: logger}
}

func (r notificationEgressRunner) Start(ctx context.Context) error {
	runners := activeNamedSchedulers(r.runners)
	if len(runners) == 0 {
		return nil
	}

	if err := r.startRunners(ctx, runners); err != nil {
		return fmt.Errorf("start runners: %w", err)
	}

	return nil
}

func activeNamedSchedulers(runners []NamedScheduler) []NamedScheduler {
	active := make([]NamedScheduler, 0, len(runners))
	for _, runner := range runners {
		if runner.Scheduler != nil {
			active = append(active, runner)
		}
	}

	return active
}

func (r youtubeOutboxDispatcherRunner) Start(ctx context.Context) error {
	if r.dispatcher == nil {
		return nil
	}

	if r.logger != nil {
		r.logger.Info("YouTube outbox dispatcher started by alarm-worker")
	}

	if err := r.dispatcher.Run(ctx); err != nil {
		return fmt.Errorf("run youtube outbox dispatcher: %w", err)
	}

	return nil
}

func (r notificationEgressRunner) startRunners(ctx context.Context, runners []NamedScheduler) error {
	if r.logger != nil {
		r.logger.Info("Notification egress owned by this alarm-worker instance",
			slog.String("event", "notification_egress_started"),
			slog.Group("runners", slog.Int("count", len(runners))),
		)
	}

	if err := runNamedSchedulers(ctx, r.logger, "notification-egress-", runners); err != nil {
		return fmt.Errorf(
			"handle runner group result: %w",
			fmt.Errorf("notification egress runner stopped: %w", err),
		)
	}

	return nil
}

func (r *AlarmWorkerRuntime) setAlarmSchedulerCancel(cancel context.CancelFunc) {
	if cancel == nil {
		return
	}

	r.schedulerMu.Lock()

	if r.schedulerDone == nil {
		r.schedulerDone = make(chan struct{})
	}

	prevCancel := r.schedulerCancel

	r.schedulerCancel = cancel
	r.schedulerMu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
}

func (r *AlarmWorkerRuntime) clearAlarmSchedulerCancel() bool {
	r.schedulerMu.Lock()

	cancel := r.schedulerCancel

	r.schedulerCancel = nil
	r.schedulerMu.Unlock()

	if cancel != nil {
		cancel()

		return true
	}

	return false
}

func (r *AlarmWorkerRuntime) beginAlarmScheduler() {
	r.schedulerMu.Lock()

	r.schedulerDone = make(chan struct{})
	r.schedulerMu.Unlock()
}

func (r *AlarmWorkerRuntime) alarmSchedulerDone() chan struct{} {
	r.schedulerMu.Lock()

	done := r.schedulerDone
	r.schedulerMu.Unlock()

	return done
}

func (r *AlarmWorkerRuntime) waitAlarmScheduler(ctx context.Context) error {
	done := r.alarmSchedulerDone()
	if done == nil {
		return nil
	}

	select {
	case <-done:
		return nil
	default:
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *AlarmWorkerRuntime) Close() {
	if r == nil {
		return
	}

	r.Managed.Close()
}
