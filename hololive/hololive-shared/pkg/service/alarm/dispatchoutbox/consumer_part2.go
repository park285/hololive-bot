package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *Consumer) RouteSendingFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	updates, retryCount, dlqCount := failureUpdatesFromEnvelopes(retryEnvelopes, dlqEnvelopes)
	if err := observeRoutedFailures(c.repository.RouteSendingFailures(ctx, updates, c.workerID), updates, retryCount, dlqCount); err != nil {
		return fmt.Errorf("observe routed failures: %w", err)
	}

	return nil
}

func (c *Consumer) RequeuePreSend(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	updates, retryCount, _ := failureUpdatesFromEnvelopes(envelopes, nil)
	if err := observeRoutedFailures(c.repository.RequeuePreSend(ctx, updates, c.workerID), updates, retryCount, 0); err != nil {
		return fmt.Errorf("observe routed failures: %w", err)
	}

	return nil
}

func observeRoutedFailures(err error, updates []FailureUpdate, retryCount, dlqCount int) error {
	if err == nil {
		observePGRetryScheduled(retryCount)
		observePGDLQ(dlqCount)

		return nil
	}

	if partial, ok := errors.AsType[*PartialTransitionError](err); ok {
		retryApplied, dlqApplied := appliedFailureCounts(updates, partial.UnappliedIDs)
		observePGRetryScheduled(retryApplied)
		observePGDLQ(dlqApplied)
	}

	return err
}

func appliedFailureCounts(updates []FailureUpdate, unappliedIDs []int64) (retryApplied, dlqApplied int) {
	unapplied := make(map[int64]struct{}, len(unappliedIDs))
	for _, id := range unappliedIDs {
		unapplied[id] = struct{}{}
	}

	for i := range updates {
		if _, ok := unapplied[updates[i].ID]; ok {
			continue
		}

		if updates[i].TargetStatus == StatusRetry {
			retryApplied++
		} else {
			dlqApplied++
		}
	}

	return retryApplied, dlqApplied
}

func failureUpdatesFromEnvelopes(retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) (updates []FailureUpdate, retryCount, dlqCount int) {
	now := time.Now().UTC()

	updates = make([]FailureUpdate, 0, len(retryEnvelopes)+len(dlqEnvelopes))
	updates = appendFailureUpdates(updates, retryEnvelopes, StatusRetry, now)
	retryCount = len(updates)
	updates = appendFailureUpdates(updates, dlqEnvelopes, StatusDLQ, now)

	return updates, retryCount, len(updates) - retryCount
}

func appendFailureUpdates(updates []FailureUpdate, envelopes []domain.AlarmQueueEnvelope, target Status, now time.Time) []FailureUpdate {
	for i := range envelopes {
		update, ok := failureUpdateFromEnvelope(&envelopes[i], now, target)
		if !ok {
			continue
		}

		updates = append(updates, update)
	}

	return updates
}

func failureUpdateFromEnvelope(envelope *domain.AlarmQueueEnvelope, now time.Time, target Status) (FailureUpdate, bool) {
	if envelope.DispatchOutboxID <= 0 {
		return FailureUpdate{}, false
	}

	update := FailureUpdate{ID: envelope.DispatchOutboxID, NextAttemptAt: now, TargetStatus: target}
	if envelope.Retry == nil {
		return update, true
	}

	update.AttemptCount = envelope.Retry.Attempt
	update.Error = sanitizeStoredError(envelope.Retry.LastError)
	update.ErrorCode = envelope.Retry.LastErrorCode

	if parsed, err := time.Parse(time.RFC3339Nano, envelope.Retry.NextVisibleAt); err == nil {
		update.NextAttemptAt = parsed.UTC()
	}

	return update, true
}

func (c *Consumer) Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if err := c.RouteFailures(ctx, envelopes, nil); err != nil {
		return fmt.Errorf("route failures: %w", err)
	}

	return nil
}

func (c *Consumer) Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	message, code := storedErrorFromCause(cause)
	updates := make([]TerminalUpdate, 0, len(envelopes))

	for i := range envelopes {
		envelope := &envelopes[i]
		if envelope.DispatchOutboxID > 0 {
			updates = append(updates, TerminalUpdate{ID: envelope.DispatchOutboxID, Error: message, ErrorCode: code})
		}
	}

	if err := c.repository.Quarantine(ctx, updates, c.workerID); err != nil {
		return fmt.Errorf("quarantine: %w", err)
	}

	observePGQuarantined(len(updates))

	return nil
}

type deliveryContext struct {
	Users []string `json:"users,omitempty"`
}

func distinctEventIDs(records []*Record) []int64 {
	seen := make(map[int64]struct{}, len(records))
	ids := make([]int64, 0, len(records))

	for _, record := range records {
		if record == nil || record.EventID <= 0 {
			continue
		}

		if _, ok := seen[record.EventID]; ok {
			continue
		}

		seen[record.EventID] = struct{}{}
		ids = append(ids, record.EventID)
	}

	return ids
}

func rehydrateDeliveryContext(envelope *domain.AlarmQueueEnvelope, record *Record) error {
	envelope.Notification.RoomID = record.RoomID
	if len(record.DeliveryContext) == 0 {
		return nil
	}

	var deliveryCtx deliveryContext

	if err := jsonv2.Unmarshal(record.DeliveryContext, &deliveryCtx); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	envelope.Notification.Users = deliveryCtx.Users

	return nil
}
