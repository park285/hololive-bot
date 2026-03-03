package scheduler

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

// NotificationLocker: delivery.NotificationLocker의 type alias (패키지 내부 호환용)
type NotificationLocker = delivery.NotificationLocker

// outboxEnqueuer: outbox enqueue 연산 인터페이스 (테스트 mock 용도)
type outboxEnqueuer interface {
	Enqueue(ctx context.Context, kind domain.DeliveryOutboxKind, periodKey, roomID, message string) error
}

// enqueueToRooms: Room별 outbox enqueue (claim 없이 DB UNIQUE로 dedup)
func enqueueToRooms(
	ctx context.Context,
	outboxRepo outboxEnqueuer,
	rooms []roomTarget,
	kind domain.DeliveryOutboxKind,
	periodKey string,
	message string,
	logger *slog.Logger,
) delivery.SendResult {
	var result delivery.SendResult

	for _, room := range rooms {
		result.Attempted++

		if err := outboxRepo.Enqueue(ctx, kind, periodKey, room.roomID, message); err != nil {
			logger.Error("Failed to enqueue notification",
				slog.String("room_id", room.roomID),
				slog.String("error", err.Error()))
			result.Failed++
			result.FailedRooms = append(result.FailedRooms, room.roomID)
			continue
		}

		result.Sent++
		logger.Info("Enqueued notification",
			slog.String("room_id", room.roomID))
	}

	return result
}

// roomTarget: enqueueToRooms에 전달할 room 정보
type roomTarget struct {
	roomID string
}
