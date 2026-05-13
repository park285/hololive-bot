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

package checker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (n *Notifier) publishBatchAndMark(ctx context.Context, items []claimedSend) (int, error) {
	notifications := make([]*domain.AlarmNotification, 0, len(items))
	claimKeys := make([][]string, 0, len(items))
	for _, item := range items {
		notifications = append(notifications, item.payload.notification)
		claimKeys = append(claimKeys, item.claimKeys)
	}
	result, err := n.queuePublisher.PublishBatch(ctx, notifications, claimKeys)
	processed := clampProcessedDeliveries(result.ProcessedDeliveries, len(items))
	if err != nil {
		n.releaseClaimsBestEffort(ctx, claimKeysFromItems(items[processed:]), "failed to release claims after queue batch publish error")
		for _, item := range items[:processed] {
			n.markPublishedBestEffort(ctx, item.payload)
		}
		return processed, fmt.Errorf("publish queue batch: %w", err)
	}
	for _, item := range items {
		n.markPublishedBestEffort(ctx, item.payload)
	}
	return len(items), nil
}

func clampProcessedDeliveries(processed int, total int) int {
	if processed < 0 {
		return 0
	}
	if processed > total {
		return total
	}
	return processed
}

func claimKeysFromItems(items []claimedSend) []string {
	keys := make([]string, 0, len(items)*2)
	for _, item := range items {
		keys = append(keys, item.claimKeys...)
	}
	return keys
}

func (n *Notifier) markPublishedBestEffort(ctx context.Context, payload *sendInput) {
	if err := n.dedupSvc.MarkAsNotified(
		ctx,
		payload.streamID,
		payload.startScheduled,
		payload.notification.MinutesUntil,
	); err != nil {
		n.logger.Warn("Failed to mark as notified after publish (non-fatal)",
			slog.String("stream_id", payload.streamID),
			slog.Int("minutes_until", payload.notification.MinutesUntil),
			slog.Any("error", err),
		)
	}

	if err := n.dedupSvc.MarkUpcomingEventNotified(
		ctx,
		payload.notification.RoomID,
		payload.channelID,
		payload.notification.Stream,
	); err != nil {
		n.logger.Warn("Failed to mark upcoming event notified after publish (non-fatal)",
			slog.String("room_id", payload.notification.RoomID),
			slog.String("channel_id", payload.channelID),
			slog.Any("error", err),
		)
	}
}

func (n *Notifier) releaseClaimsBestEffort(ctx context.Context, claimKeys []string, message string) {
	if len(claimKeys) == 0 {
		return
	}

	if err := n.dedupSvc.ReleaseClaims(ctx, claimKeys); err != nil {
		n.logger.Warn(message, slog.Any("error", err))
	}
}
