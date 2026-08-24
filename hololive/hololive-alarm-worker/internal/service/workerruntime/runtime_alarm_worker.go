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

package workerruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
)

type Scheduler interface {
	Start(ctx context.Context) error
}

type AlarmWorkerRuntime struct {
	lifecycle.Managed

	Config *settings.Config
	Logger *slog.Logger

	Scheduler            Scheduler
	NotificationEgress   Scheduler
	CelebrationRunner    Scheduler
	BirthdayStreamRunner Scheduler
	ConfigSubscriber     *configsub.Subscriber
	ServerAddr           string
	HTTPServers          *sharedserver.RuntimeHTTPServers
	AlarmService         interface{ Close(context.Context) error }
	WorkerObservability  interface{ Start(context.Context) }

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
		return fmt.Errorf("run: %w", err)
	}

	return nil
}

func (r notificationEgressRunner) startRunners(ctx context.Context, runners []NamedScheduler) error {
	if r.logger != nil {
		names := make([]string, 0, len(runners))
		for _, runner := range runners {
			names = append(names, runner.Name)
		}

		r.logger.Info("Notification egress owned by this alarm-worker instance",
			slog.String("event", "notification_egress_started"),
			slog.Any("runners", names),
		)
	}

	runnerErrCh := r.startRunnerGroup(ctx, runners)

	if err := r.handleRunnerGroupResult(<-runnerErrCh); err != nil {
		return fmt.Errorf("handle runner group result: %w", err)
	}

	return nil
}

func (r notificationEgressRunner) handleRunnerGroupResult(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("notification egress runner stopped: %w", err)
}

func (r notificationEgressRunner) startRunnerGroup(ctx context.Context, runners []NamedScheduler) <-chan error {
	ch := make(chan error, 1)

	panicguard.Go(r.logger, "notification-egress-runner-group", func() {
		ch <- panicguard.RunE(r.logger, "notification-egress-runner-group", func() error {
			eg, egCtx := errgroup.WithContext(ctx)

			for _, runner := range runners {
				panicguard.GoE(eg, r.logger, "notification-egress-"+runner.Name, func() error {
					return runner.Scheduler.Start(egCtx)
				})
			}

			return eg.Wait()
		})
	})

	return ch
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

func (r *AlarmWorkerRuntime) waitAlarmScheduler(ctx context.Context) {
	done := r.alarmSchedulerDone()
	if done == nil {
		return
	}

	select {
	case <-done:
	case <-ctx.Done():
	}
}
