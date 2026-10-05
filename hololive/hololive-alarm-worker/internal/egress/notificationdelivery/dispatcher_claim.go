package notificationdelivery

import (
	"context"
	"log/slog"
	"sync"

	"github.com/park285/shared-go/v2/pkg/panicguard"
)

// 실행 슬롯이 있는 방의 첫 항목만 claim합니다. 대기 항목을 미리 임대하지 않아
// 방별 순서를 기다리는 시간이 PENDING lease를 소비하지 않습니다.
// 짧은 backoff로 다시 due가 된 ID도 같은 poll에서는 한 번만 처리합니다.
func (d *Dispatcher) processReady(ctx context.Context) {
	remaining := d.config.BatchSize
	concurrency := min(d.config.MaxConcurrent, remaining)
	activeRooms := make(map[string]struct{}, concurrency)
	completed := make(chan string, concurrency)
	processedIDs := make([]int64, 0, d.config.BatchSize)

	var workers sync.WaitGroup

	defer workers.Wait()

	for remaining > 0 && ctx.Err() == nil {
		drainCompletedDeliveries(completed, activeRooms)

		available := min(concurrency-len(activeRooms), remaining)
		if available > 0 {
			excludedRooms := make([]string, 0, len(activeRooms))
			for roomID := range activeRooms {
				excludedRooms = append(excludedRooms, roomID)
			}

			items, err := d.repository.fetchReadyAndLock(ctx, d.workerID, available, deliveryLease, excludedRooms, processedIDs)
			if err != nil {
				d.logger.Error("Failed to fetch outbox items", slog.String("error", err.Error()))

				return
			}

			if len(items) > 0 {
				for i := range items {
					item := items[i]

					activeRooms[item.RoomID] = struct{}{}
					processedIDs = append(processedIDs, item.ID)
					remaining--

					workers.Go(func() {
						defer func() { completed <- item.RoomID }()

						panicguard.Run(d.logger, panicguard.BackgroundTask, "delivery-dispatch-item", func() {
							d.processItem(ctx, &item)
						})
					})
				}

				continue
			}
		}

		if len(activeRooms) == 0 {
			return
		}

		select {
		case roomID := <-completed:
			delete(activeRooms, roomID)
		case <-ctx.Done():
			return
		}
	}
}

func drainCompletedDeliveries(completed <-chan string, activeRooms map[string]struct{}) {
	for {
		select {
		case roomID := <-completed:
			delete(activeRooms, roomID)
		default:
			return
		}
	}
}
