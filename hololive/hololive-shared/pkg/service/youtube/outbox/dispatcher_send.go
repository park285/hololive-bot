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
	"fmt"
	"log/slog"
	"strings"
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

type itemSendResult struct {
	roomsSent  int
	sendErrors []string
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

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.deliveryParallelism())
	for i := range rows {
		row := rows[i]
		eg.Go(func() error {
			d.dispatchDeliveryRow(egCtx, row, outboxByID, &result, &mu)
			return nil
		})
	}
	_ = eg.Wait()

	return result
}

func (d *Dispatcher) dispatchDeliveryRow(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	item, ok := outboxByID[row.OutboxID]
	if !ok {
		d.recordDeliveryFailure(result, mu, "outbox row not found", row.ID, row.OutboxID)
		return
	}

	message, formatErr := d.formatter.formatMessage(ctx, item)
	if formatErr != nil {
		d.logger.Warn("Failed to format delivery message",
			slog.Int64("delivery_id", row.ID),
			slog.Int64("outbox_id", row.OutboxID),
			slog.Any("error", formatErr))
		d.recordDeliveryFailure(result, mu, "format message", row.ID, row.OutboxID)
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

// processItem: 개별 알림 처리
func (d *Dispatcher) processItem(ctx context.Context, item domain.YouTubeNotificationOutbox) error {
	subscribers, err := d.loadItemSubscribers(ctx, item)
	if err != nil {
		return d.failItemForSubscribers(ctx, item, err)
	}

	if len(subscribers) == 0 {
		d.logger.Debug("No subscribers for channel, skipping",
			slog.String("channel_id", item.ChannelID))
		d.markSent(ctx, item.ID)
		return nil
	}

	message, err := d.formatter.formatMessage(ctx, item)
	if err != nil {
		d.markFailed(ctx, item.ID, fmt.Sprintf("failed to format message: %v", err))
		return fmt.Errorf("failed to format message for item %d: %w", item.ID, err)
	}

	sendResult := d.sendItemMessageToRooms(ctx, subscribers, message)
	if len(sendResult.sendErrors) > 0 && sendResult.roomsSent == 0 {
		errMsg := strings.Join(sendResult.sendErrors, "; ")
		d.markFailed(ctx, item.ID, errMsg)
		return fmt.Errorf("all sends failed: %s", errMsg)
	}
	if len(sendResult.sendErrors) > 0 {
		errMsg := strings.Join(sendResult.sendErrors, "; ")
		d.markFailed(ctx, item.ID, errMsg)
		return fmt.Errorf("partial sends failed: %s", errMsg)
	}

	d.markSent(ctx, item.ID)
	d.logger.Info("Outbox notification sent",
		slog.Int64("id", item.ID),
		slog.String("kind", string(item.Kind)),
		slog.String("channel_id", item.ChannelID),
		slog.Int("rooms_sent", sendResult.roomsSent))

	return nil
}

func (d *Dispatcher) loadItemSubscribers(ctx context.Context, item domain.YouTubeNotificationOutbox) ([]string, error) {
	alarmType := item.Kind.ToAlarmType()
	subscribers, err := d.getChannelSubscribers(ctx, item.ChannelID, alarmType)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscribers for channel %s: %w", item.ChannelID, err)
	}
	return subscribers, nil
}

func (d *Dispatcher) failItemForSubscribers(ctx context.Context, item domain.YouTubeNotificationOutbox, err error) error {
	d.markFailed(ctx, item.ID, fmt.Sprintf("failed to get subscribers: %v", err))
	return err
}

func (d *Dispatcher) sendItemMessageToRooms(ctx context.Context, subscribers []string, message string) itemSendResult {
	result := itemSendResult{}
	var mu sync.Mutex

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.deliveryParallelism())
	for roomID := range d.groupByRoom(subscribers) {
		eg.Go(func() error {
			if err := d.sender.SendMessage(egCtx, roomID, message); err != nil {
				d.logger.Warn("Failed to send message to room",
					slog.String("room_id", roomID),
					slog.Any("error", err))
				mu.Lock()
				result.sendErrors = append(result.sendErrors, fmt.Sprintf("room=%s: %v", roomID, err))
				mu.Unlock()
				return nil
			}
			mu.Lock()
			result.roomsSent++
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()

	return result
}

func (d *Dispatcher) deliveryParallelism() int {
	if d.cfg.DeliveryParallelism > 0 {
		return d.cfg.DeliveryParallelism
	}
	return DefaultConfig().DeliveryParallelism
}
