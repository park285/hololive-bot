package alarmdispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

const (
	alarmDispatchBacklogObservationInterval = 60 * time.Second
	alarmDispatchBacklogObservationTimeout  = 10 * time.Second
)

var alarmDispatchBacklogStatuses = []dispatchoutbox.Status{
	dispatchoutbox.StatusPending,
	dispatchoutbox.StatusRetry,
	dispatchoutbox.StatusLeased,
	dispatchoutbox.StatusSending,
}

type alarmDispatchBacklogSnapshotStore interface {
	BacklogSnapshot(ctx context.Context) (alarmDispatchBacklogSnapshot, error)
}

type alarmDispatchBacklogObserver struct {
	store        alarmDispatchBacklogSnapshotStore
	interval     time.Duration
	queryTimeout time.Duration
	logger       *slog.Logger
}

func NewBacklogObserver(infra *sharedmodules.InfraModule, logger *slog.Logger) Scheduler {
	return &alarmDispatchBacklogObserver{
		store:        alarmDispatchMaintenancePgxStore{db: infra.Postgres.GetPool()},
		interval:     alarmDispatchBacklogObservationInterval,
		queryTimeout: alarmDispatchBacklogObservationTimeout,
		logger:       logger,
	}
}

func (o *alarmDispatchBacklogObserver) Start(ctx context.Context) error {
	for {
		o.observeOnce(ctx)

		if !retry.Sleep(ctx, o.interval) {
			return nil
		}
	}
}

func (o *alarmDispatchBacklogObserver) observeOnce(ctx context.Context) {
	observeCtx, cancelObserve := context.WithTimeout(ctx, o.queryTimeout)
	err := o.observeBacklog(observeCtx)

	cancelObserve()

	if err == nil || ctx.Err() != nil {
		return
	}

	observeAlarmDispatchBacklogObservationFailure()

	if o.logger != nil {
		o.logger.Warn("alarm dispatch backlog observation failed", slog.Any("error", err))
	}
}

func (o *alarmDispatchBacklogObserver) observeBacklog(ctx context.Context) error {
	snapshot, err := o.store.BacklogSnapshot(ctx)
	if err != nil {
		observeAlarmDispatchBacklogSnapshotFailed()

		return fmt.Errorf("backlog snapshot: %w", err)
	}

	for _, status := range alarmDispatchBacklogStatuses {
		observeAlarmDispatchBacklogStatus(string(status), snapshot.RowsByStatus[status])
	}

	observeAlarmDispatchOldestAges(
		snapshot.OldestPendingAgeSeconds,
		snapshot.OldestRetryAgeSeconds,
		snapshot.OldestSendingAgeSeconds,
	)
	observeAlarmDispatchQuarantine(&snapshot)
	observeAlarmDispatchBacklogSnapshotSucceeded(time.Now())

	return nil
}
