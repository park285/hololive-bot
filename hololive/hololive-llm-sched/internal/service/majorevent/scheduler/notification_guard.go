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

package scheduler

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

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
