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

package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func (d *Dispatcher) processItem(ctx context.Context, item *domain.NotificationDeliveryOutbox) {
	if d == nil || item == nil {
		return
	}

	var attemptID uint64

	if d.workerTracker != nil {
		attemptID = d.workerTracker.BeginAttempt(time.Now())
	}

	outcome := workercontract.AttemptFailed

	defer func() {
		if d.workerTracker != nil {
			d.workerTracker.EndAttempt(attemptID)
		}

		if d.workerTotals != nil {
			d.workerTotals.RecordAttempt(outcome)
		}
	}()

	var p outboxPayload

	if err := jsonv2.Unmarshal([]byte(item.Payload), &p); err != nil {
		d.logger.Error("Failed to unmarshal outbox payload",
			slog.Int64("id", item.ID),
			slog.String("error", err.Error()))
		d.markItemFailed(ctx, item.ID, item.LockedAt.Time, "payload unmarshal: "+err.Error())

		return
	}

	if !d.markItemSending(ctx, item.ID) {
		return
	}

	if err := d.sendMessage(ctx, item, p.Message); err != nil {
		outcome = deliveryAttemptFailure(err)
		d.logger.Error("Failed to send outbox message",
			slog.Int64("id", item.ID),
			privacylog.RoomIDAttr(item.RoomID),
			slog.String("error", err.Error()))
		d.markItemFailed(ctx, item.ID, item.LockedAt.Time, err.Error())

		return
	}

	if d.markItemSent(ctx, item.ID, item.LockedAt.Time) {
		outcome = workercontract.AttemptSuccess
	}
}

func deliveryAttemptFailure(err error) workercontract.AttemptOutcome {
	if errors.Is(err, context.DeadlineExceeded) {
		return workercontract.AttemptTimeout
	}

	if errors.Is(err, context.Canceled) {
		return workercontract.AttemptCanceled
	}

	return workercontract.AttemptFailed
}

func (d *Dispatcher) markItemSending(ctx context.Context, id int64) bool {
	fenced, err := d.repository.MarkSending(ctx, id, d.workerID, deliveryLease)
	if err != nil {
		d.logger.Error("Failed to mark outbox item sending", slog.Int64("id", id), slog.String("error", err.Error()))

		return false
	}

	if !fenced {
		d.logger.Warn("Outbox item fence skipped sending transition", slog.Int64("id", id))

		return false
	}

	return true
}

func (d *Dispatcher) markItemSent(ctx context.Context, id int64, lockedAt time.Time) bool {
	fenced, err := d.repository.MarkSent(ctx, id, d.workerID, lockedAt)
	if err != nil {
		d.logger.Error("Failed to mark outbox item as sent", slog.Int64("id", id), slog.String("error", err.Error()))

		return false
	}

	if !fenced {
		d.logger.Warn("Outbox item re-claimed before mark sent; fence skipped transition", slog.Int64("id", id))

		return false
	}

	return true
}

func (d *Dispatcher) markItemFailed(ctx context.Context, id int64, lockedAt time.Time, reason string) {
	fenced, err := d.repository.MarkFailed(ctx, id, d.workerID, lockedAt, d.config.MaxRetries, d.config.RetryBackoff, reason)
	if err != nil {
		d.logger.Error("Failed to mark outbox item failed", slog.Int64("id", id), slog.String("error", err.Error()))

		return
	}

	if !fenced {
		d.logger.Warn("Outbox item re-claimed before mark failed; fence skipped transition", slog.Int64("id", id))
	}
}

func (d *Dispatcher) sendMessage(ctx context.Context, item *domain.NotificationDeliveryOutbox, message string) error {
	if sender, ok := d.sender.(ClientRequestMessageSender); ok {
		if err := sender.SendMessageWithClientRequestID(ctx, item.RoomID, message, notificationDeliveryClientRequestID(item)); err != nil {
			return fmt.Errorf("send message with client request ID: %w", err)
		}

		return nil
	}

	if err := d.sender.SendMessage(ctx, item.RoomID, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

// 이 결정적 ID는 Iris reply admission store의 멱등 키라, 재전송돼도 카톡 중복 송출이 막힌다.
// 단 이 안전망은 Iris admission retention(168h) > outbox lease(lock_expires_at, 60s)일 때만 성립하며,
// 대소가 뒤집히면 재전송분이 dedup window 밖이라 사용자에게 중복 알림이 간다.
func notificationDeliveryClientRequestID(item *domain.NotificationDeliveryOutbox) string {
	kind := ""
	contentID := ""
	roomID := ""

	if item != nil {
		kind = string(item.Kind)
		contentID = item.ContentID
		roomID = item.RoomID
	}

	if contentID == "" && item != nil {
		contentID = strconv.FormatInt(item.ID, 10)
	}

	sum := sha256.Sum256([]byte(kind + "\x00" + contentID + "\x00" + roomID))

	return "hololive-delivery:" + hex.EncodeToString(sum[:16])
}
