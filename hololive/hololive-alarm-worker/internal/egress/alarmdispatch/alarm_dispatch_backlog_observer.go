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

// backlog·격리 gauge는 retention 주기와 무관하게 이 주기로 갱신한다. HololiveOutboxStale(60초 초과가 10분 지속)이
// 현재 backlog를 평가할 표본을 10분 창마다 충분히 남기면서, 보존 격리 행 수에 비례하는 격리 집계 조회 비용을 제한한다.
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

// 관측 실패는 Start 오류가 아니다. 첫 오류가 나면 runNamedSchedulers가 형제 egress runner를 모두 취소한다.
type alarmDispatchBacklogObserver struct {
	store        alarmDispatchBacklogSnapshotStore
	interval     time.Duration
	queryTimeout time.Duration
	logger       *slog.Logger
}

// NewBacklogObserver는 PostgreSQL pool이 있는 infra를 받는다. 조립 쪽(buildNotificationEgress)이 기동 때 pool을 먼저 확인한다.
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
