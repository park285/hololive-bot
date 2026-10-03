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

package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func (n *Notifier) publishBatchAndMark(ctx context.Context, items []claimedSend) (int, error) {
	if items == nil {
		items = []claimedSend{}
	}

	notifications := make([]*domain.AlarmNotification, 0, len(items))
	claimKeys := make([][]string, 0, len(items))

	for _, item := range items {
		notifications = append(notifications, item.payload.notification)
		claimKeys = append(claimKeys, item.claimKeys)
	}

	result, err := n.queuePublisher.PublishBatch(ctx, notifications, claimKeys)
	accepted, terminal := publishReceiptResults(result.Receipts, len(items))

	published := 0

	for i, item := range items {
		if accepted[i] {
			n.markPublishedBestEffort(ctx, item.payload)

			if n.tierScheduler != nil {
				n.tierScheduler.MarkChannelRecentlyNotified(item.payload.channelID)
			}

			published++
		} else if !terminal[i] {
			// 비성공 terminal의 기존 claim은 보존하고, 충돌·미확정 입력만 재평가 가능하게 한다.
			n.releaseClaimsBestEffort(ctx, item.claimKeys, "failed to release unaccepted publish claims")
		}
	}

	if published != len(items) && err == nil {
		err = errors.New("one or more deliveries were not accepted")
	}

	if err != nil {
		return published, fmt.Errorf("publish queue batch: %w", err)
	}

	return published, nil
}

func publishReceiptResults(receipts []dispatchoutbox.PublishReceipt, itemCount int) (accepted, terminal map[int]bool) {
	accepted = make(map[int]bool, len(receipts))
	terminal = make(map[int]bool, len(receipts))

	for _, receipt := range receipts {
		if receipt.Ordinal < 0 || receipt.Ordinal >= itemCount {
			continue
		}

		accepted[receipt.Ordinal] = receipt.Accepted()
		terminal[receipt.Ordinal] = receipt.Outcome == dispatchoutbox.PublishRejectedTerminal
	}

	return accepted, terminal
}

func (n *Notifier) markPublishedBestEffort(ctx context.Context, payload *sendInput) {
	if err := n.dedupService.MarkAsNotified(
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

	if err := n.dedupService.MarkUpcomingEventNotified(
		ctx,
		payload.notification.RoomID,
		payload.channelID,
		payload.notification.Stream,
	); err != nil {
		n.logger.Warn("Failed to mark upcoming event notified after publish (non-fatal)",
			privacylog.RoomIDAttr(payload.notification.RoomID),
			slog.String("channel_id", payload.channelID),
			slog.Any("error", err),
		)
	}
}

func (n *Notifier) releaseClaimsBestEffort(ctx context.Context, claimKeys []string, message string) {
	if len(claimKeys) == 0 {
		return
	}

	if err := n.dedupService.ReleaseClaims(ctx, claimKeys); err != nil {
		n.logger.Warn(message, slog.Any("error", err))
	}
}
