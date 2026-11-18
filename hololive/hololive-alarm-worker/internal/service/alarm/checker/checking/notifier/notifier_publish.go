package notifier

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
	marked := make(notifiedWrites, len(items))

	for i, item := range items {
		if accepted[i] {
			if marked.claim(item.payload.streamID, item.payload.startScheduled, item.payload.notification.MinutesUntil) {
				n.markNotifiedBestEffort(ctx, item.payload)
			}

			n.markUpcomingEventBestEffort(ctx, item.payload)

			published++
		} else if !terminal[i] {
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

type notifiedWrites map[string]notifiedSchedule

type notifiedSchedule struct {
	startScheduled time.Time
	minutes        map[int]struct{}
}

func (w notifiedWrites) claim(streamID string, startScheduled time.Time, minutesUntil int) bool {
	schedule, ok := w[streamID]
	if !ok || !schedule.startScheduled.Equal(startScheduled) {
		schedule = notifiedSchedule{startScheduled: startScheduled, minutes: map[int]struct{}{}}
		w[streamID] = schedule
	}

	if _, done := schedule.minutes[minutesUntil]; done {
		return false
	}

	schedule.minutes[minutesUntil] = struct{}{}

	return true
}

func (n *Notifier) markNotifiedBestEffort(ctx context.Context, payload *sendInput) {
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
}

func (n *Notifier) markUpcomingEventBestEffort(ctx context.Context, payload *sendInput) {
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
