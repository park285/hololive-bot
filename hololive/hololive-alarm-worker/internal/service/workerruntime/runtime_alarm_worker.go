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
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	youtubeoutbox "github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-shared/pkg/ctxutil"
)

type Scheduler interface {
	Start(ctx context.Context) error
}

type AlarmWorkerRuntime struct {
	Config *config.Config
	Logger *slog.Logger

	Scheduler            Scheduler
	NotificationEgress   Scheduler
	CelebrationRunner    Scheduler
	BirthdayStreamRunner Scheduler
	ConfigSubscriber     *configsub.Subscriber
	ServerAddr           string
	HTTPServers          *sharedserver.RuntimeHTTPServers

	schedulerMu     sync.Mutex
	schedulerCancel context.CancelFunc
	lifecycle.Managed
}

type NamedScheduler struct {
	Name      string
	Scheduler Scheduler
}

type notificationEgressRunner struct {
	runners            []NamedScheduler
	leaseCache         cache.Client
	leaseEnabled       bool
	leaseRetryInterval time.Duration
	logger             *slog.Logger
}

type youtubeOutboxDispatcherRunner struct {
	dispatcher *youtubeoutbox.Dispatcher
	logger     *slog.Logger
}

func NewNotificationEgressRunner(
	runners []NamedScheduler,
	leaseCache cache.Client,
	leaseEnabled bool,
	logger *slog.Logger,
) Scheduler {
	return notificationEgressRunner{
		runners:      runners,
		leaseCache:   leaseCache,
		leaseEnabled: leaseEnabled,
		logger:       logger,
	}
}

func NewYouTubeOutboxDispatcherRunner(dispatcher *youtubeoutbox.Dispatcher, logger *slog.Logger) Scheduler {
	return youtubeOutboxDispatcherRunner{dispatcher: dispatcher, logger: logger}
}

const notificationEgressLeaseAcquireRetryInterval = time.Duration(egress.NotificationEgressLeaseAcquireRetrySeconds) * time.Second

func (r notificationEgressRunner) Start(ctx context.Context) error {
	runners := activeNamedSchedulers(r.runners)
	if len(runners) == 0 {
		return nil
	}
	if r.leaseEnabled {
		return r.startWithLease(ctx, runners)
	}
	return r.startRunners(ctx, runners)
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
	r.dispatcher.Start(ctx)
	if r.logger != nil {
		r.logger.Info("YouTube outbox dispatcher started by alarm-worker")
	}
	<-ctx.Done()
	return nil
}

func (r notificationEgressRunner) startWithLease(ctx context.Context, runners []NamedScheduler) error {
	for ctx.Err() == nil {
		lease, err := r.acquireLease(ctx)
		if err != nil {
			return err
		}
		if lease != nil {
			return r.startLeaseProtectedRunners(ctx, runners, lease)
		}
		if !ctxutil.SleepWithContext(ctx, r.effectiveLeaseRetryInterval()) {
			return nil
		}
	}
	return nil
}

func (r notificationEgressRunner) effectiveLeaseRetryInterval() time.Duration {
	if r.leaseRetryInterval > 0 {
		return r.leaseRetryInterval
	}
	return notificationEgressLeaseAcquireRetryInterval
}

func (r notificationEgressRunner) startLeaseProtectedRunners(
	ctx context.Context,
	runners []NamedScheduler,
	lease *egress.NotificationEgressLease,
) error {
	if lease == nil {
		waitForContextDone(ctx)
		return nil
	}
	defer r.releaseLease(ctx, lease)

	renewErrCh := r.startLeaseRenewLoop(ctx, lease)
	runnerErrCh := r.startRunnerGroup(ctx, runners)
	select {
	case <-ctx.Done():
		return nil
	case err := <-runnerErrCh:
		return r.handleRunnerGroupResult(err)
	case err := <-renewErrCh:
		return r.handleLeaseRenewLoopResult(err)
	}
}

func waitForContextDone(ctx context.Context) {
	<-ctx.Done()
}

func (r notificationEgressRunner) startRunners(ctx context.Context, runners []NamedScheduler) error {
	runnerErrCh := r.startRunnerGroup(ctx, runners)
	select {
	case <-ctx.Done():
		return nil
	case err := <-runnerErrCh:
		return r.handleRunnerGroupResult(err)
	}
}

func (r notificationEgressRunner) handleRunnerGroupResult(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("notification egress runner stopped: %w", err)
}

func (r notificationEgressRunner) handleLeaseRenewLoopResult(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("notification egress runner stopped: notification egress lease renew failed: %w", err)
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

func (r notificationEgressRunner) startLeaseRenewLoop(ctx context.Context, lease *egress.NotificationEgressLease) <-chan error {
	ch := make(chan error, 1)
	panicguard.Go(r.logger, "notification-egress-lease-renew", func() {
		ch <- panicguard.RunE(r.logger, "notification-egress-lease-renew", func() error {
			return lease.RenewLoop(ctx)
		})
	})
	return ch
}

func (r notificationEgressRunner) acquireLease(ctx context.Context) (*egress.NotificationEgressLease, error) {
	lease, err := egress.AcquireNotificationEgressLease(ctx, r.leaseCache, r.logger)
	if err == nil {
		return lease, nil
	}
	if errors.Is(err, egress.ErrNotificationEgressLeaseHeld) {
		if r.logger != nil {
			r.logger.Warn("Notification egress disabled because notification egress lease is held",
				slog.String("lease_key", egress.NotificationEgressLeaseKey),
				slog.Any("error", err),
			)
		}
		return nil, nil
	}
	return nil, err
}

func (r notificationEgressRunner) releaseLease(ctx context.Context, lease *egress.NotificationEgressLease) {
	if !r.leaseEnabled || lease == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := lease.Release(ctx); err != nil && r.logger != nil {
		r.logger.Warn("Failed to release notification egress lease", slog.Any("error", err))
	}
}

func (r *AlarmWorkerRuntime) setAlarmSchedulerCancel(cancel context.CancelFunc) {
	if cancel == nil {
		return
	}

	r.schedulerMu.Lock()
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
