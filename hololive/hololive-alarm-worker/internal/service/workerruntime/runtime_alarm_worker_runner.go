package workerruntime

import (
	"context"
	"fmt"

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
)

func (r *AlarmWorkerRuntime) Run() error {
	if r == nil {
		return nil
	}

	if err := applifecycle.Run(r.Logger, r.Start, r.Shutdown); err != nil {
		return fmt.Errorf("run alarm worker lifecycle: %w", err)
	}

	return nil
}

func (r *AlarmWorkerRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}

	r.beginAlarmScheduler()

	startAlarmScheduler(ctx, errCh, alarmSchedulerHooks{
		Logger:    r.Logger,
		Start:     r.startBackgroundSchedulers,
		SetCancel: r.setAlarmSchedulerCancel,
	})

	applifecycle.Start(ctx, errCh, applifecycle.StartHooks{
		Logger:          r.Logger,
		ServerAddr:      r.ServerAddr,
		StartHTTPServer: r.StartHTTPServer,
	})
}

func (r *AlarmWorkerRuntime) startBackgroundSchedulers(ctx context.Context) error {
	done := r.alarmSchedulerDone()
	if done != nil {
		defer close(done)
	}

	runners := []NamedScheduler{
		{Name: "observability", Scheduler: r.WorkerObservability},
		{Name: "scheduler", Scheduler: r.Scheduler},
		{Name: "notification-egress", Scheduler: r.NotificationEgress},
		{Name: "celebration", Scheduler: r.CelebrationRunner},
		{Name: "x-spaces", Scheduler: r.XSpacesRunner},
		{Name: "event-grade", Scheduler: r.EventGradeRunner},
	}

	return runNamedSchedulers(ctx, r.Logger, "alarm-worker-", runners)
}

func (r *AlarmWorkerRuntime) StartHTTPServer(errCh chan<- error) {
	if r == nil {
		return
	}

	r.HTTPServers.Start(r.Logger, errCh)
}
