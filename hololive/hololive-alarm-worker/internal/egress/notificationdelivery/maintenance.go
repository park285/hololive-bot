package notificationdelivery

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	// FAILED 누적은 경보용 관측이라 발송 poll마다 전체 행을 세지 않고 이 주기로 정확한 count를 남긴다.
	deliveryFailedCountInterval       = 5 * time.Minute
	deliveryFailedCountAlertThreshold = 5
)

// maintenanceTask는 유지보수 loop 한 goroutine 안에서만 다룬다. 작업이 순서대로 하나씩 돌아 서로 겹치지 않고,
// 유지보수가 동시에 쓰는 DB 연결은 최대 하나다.
type maintenanceTask struct {
	name     string
	interval time.Duration
	run      func(context.Context) error
	nextAt   time.Time
}

func (d *Dispatcher) maintenanceTasks() []*maintenanceTask {
	tasks := []*maintenanceTask{
		{name: "stale-sending-sweep", interval: d.config.StaleSendingSweepInterval, run: d.quarantineStaleSending},
		{name: "failed-count", interval: deliveryFailedCountInterval, run: d.logAccumulatedFailures},
	}

	if d.config.CleanupEnabled {
		tasks = append(tasks, &maintenanceTask{name: "cleanup", interval: d.config.CleanupInterval, run: d.cleanup})
	}

	return tasks
}

// runMaintenanceLoop은 시작 즉시 모든 작업을 한 번 실행하고, 이후 각 작업의 다음 예정 시각까지 기다린다.
// 실패한 작업은 성공 주기보다 길지 않은 poll 주기 뒤에 다시 시도한다.
func (d *Dispatcher) runMaintenanceLoop(ctx context.Context) {
	tasks := d.maintenanceTasks()

	for {
		next := d.runDueMaintenance(ctx, tasks)

		wait := time.NewTimer(time.Until(next))

		select {
		case <-ctx.Done():
			wait.Stop()

			return
		case <-wait.C:
		}
	}
}

func (d *Dispatcher) runDueMaintenance(ctx context.Context, tasks []*maintenanceTask) time.Time {
	var next time.Time

	for _, task := range tasks {
		if ctx.Err() != nil {
			return time.Now()
		}

		if !time.Now().Before(task.nextAt) {
			d.runMaintenanceTask(ctx, task)
		}

		if next.IsZero() || task.nextAt.Before(next) {
			next = task.nextAt
		}
	}

	return next
}

func (d *Dispatcher) runMaintenanceTask(ctx context.Context, task *maintenanceTask) {
	taskCtx, cancel := context.WithTimeout(ctx, deliveryMaintenanceTimeout)
	err := task.run(taskCtx)

	cancel()

	if err != nil {
		task.nextAt = time.Now().Add(min(task.interval, d.config.PollInterval))

		return
	}

	task.nextAt = time.Now().Add(task.interval)
}

func (d *Dispatcher) logAccumulatedFailures(ctx context.Context) error {
	cnt, err := d.repository.CountByStatus(ctx, domain.DeliveryStatusFailed)
	if err != nil {
		d.logger.Warn("Failed to count delivery outbox failures", slog.String("error", err.Error()))

		return fmt.Errorf("count failed deliveries: %w", err)
	}

	if cnt > deliveryFailedCountAlertThreshold {
		d.logger.Error("delivery outbox accumulated failures", slog.Int64("count", cnt))
	}

	return nil
}

func (d *Dispatcher) cleanup(ctx context.Context) error {
	cleaned, err := d.repository.Cleanup(ctx, d.config.CleanupAfter)
	if err != nil {
		d.logger.Warn("Outbox cleanup failed", slog.String("error", err.Error()))

		return fmt.Errorf("cleanup deliveries: %w", err)
	}

	if cleaned > 0 {
		d.logger.Info("Outbox cleanup completed", slog.Int64("removed", cleaned))
	}

	return nil
}

func (d *Dispatcher) quarantineStaleSending(ctx context.Context) error {
	quarantined, err := d.repository.QuarantineStaleSending(ctx, d.config.StaleSendingAfter, d.config.StaleSendingSweepLimit)
	if err != nil {
		d.logger.Warn("Stale sending outbox sweep failed", slog.String("error", err.Error()))

		return fmt.Errorf("quarantine stale sending deliveries: %w", err)
	}

	if quarantined > 0 {
		d.logger.Warn("Stale sending outbox rows quarantined", slog.Int64("count", quarantined))
	}

	return nil
}
