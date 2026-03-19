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

package outbox

import (
	"context"
	"log/slog"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type deliveryDispatchResult struct {
	successDeliveryIDs []int64
	touchedOutboxIDs   []int64
	failedDeliveries   int
	failureBuckets     map[string][]int64
}

func (d *Dispatcher) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) deliveryDispatchResult {
	result := deliveryDispatchResult{
		successDeliveryIDs: make([]int64, 0, len(rows)),
		touchedOutboxIDs:   make([]int64, 0, len(rows)),
		failureBuckets:     make(map[string][]int64),
	}
	var mu sync.Mutex

	formattedMessages, formatFailures := d.preFormatMessages(ctx, outboxByID)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.deliveryParallelism())
	for i := range rows {
		row := rows[i]
		eg.Go(func() error {
			d.dispatchDeliveryRow(egCtx, row, formattedMessages, formatFailures, &result, &mu)
			return nil
		})
	}
	_ = eg.Wait()

	return result
}

func (d *Dispatcher) dispatchDeliveryRow(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	if formatFailures[row.OutboxID] {
		d.recordDeliveryFailure(result, mu, "format message", row.ID, row.OutboxID)
		return
	}
	message, ok := formattedMessages[row.OutboxID]
	if !ok {
		d.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
		return
	}

	if sendErr := d.sender.SendMessage(ctx, row.RoomID, message); sendErr != nil {
		d.logger.Warn("Failed to send per-room delivery",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
			slog.String("room_id", row.RoomID),
			slog.Any("error", sendErr))
		d.recordDeliveryFailure(result, mu, "send message", row.ID, row.OutboxID)
		return
	}

	mu.Lock()
	result.successDeliveryIDs = append(result.successDeliveryIDs, row.ID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, row.OutboxID)
	mu.Unlock()
}

func (d *Dispatcher) recordDeliveryFailure(
	result *deliveryDispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
) {
	mu.Lock()
	result.failedDeliveries++
	result.failureBuckets[reason] = append(result.failureBuckets[reason], deliveryID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, outboxID)
	mu.Unlock()
}

// preFormatMessages: outbox_id별로 메시지를 1회 포맷하여 캐싱
func (d *Dispatcher) preFormatMessages(ctx context.Context, outboxByID map[int64]domain.YouTubeNotificationOutbox) (messages map[int64]string, failures map[int64]bool) {
	messages = make(map[int64]string, len(outboxByID))
	failures = make(map[int64]bool)
	for id, item := range outboxByID {
		msg, err := d.formatter.formatMessage(ctx, item)
		if err != nil {
			d.logger.Warn("Failed to pre-format outbox message",
				slog.Int64("outbox_id", id),
				slog.Any("error", err))
			failures[id] = true
			continue
		}
		messages[id] = msg
	}
	return
}

func (d *Dispatcher) deliveryParallelism() int {
	if d.cfg.DeliveryParallelism > 0 {
		return d.cfg.DeliveryParallelism
	}
	return DefaultConfig().DeliveryParallelism
}
