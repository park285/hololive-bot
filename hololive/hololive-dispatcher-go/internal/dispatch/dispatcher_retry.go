package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (d *Dispatcher) handleDispatchFailure(
	ctx context.Context,
	roomID string,
	envelopes []domain.AlarmQueueEnvelope,
	failureKind string,
	dispatchErr error,
) error {
	if len(envelopes) == 0 {
		return nil
	}

	failureMessage := formatDispatchFailure(failureKind, dispatchErr)
	batches := d.dispatchFailureBatches(envelopes, failureMessage)

	if err := d.persistDispatchRetries(ctx, roomID, failureKind, batches.retryEnvelopes, batches.retryBackoffs); err != nil {
		return err
	}
	return d.persistDispatchDLQ(ctx, roomID, failureKind, batches.dlqEnvelopes)
}

type dispatchFailureBatches struct {
	retryEnvelopes []domain.AlarmQueueEnvelope
	dlqEnvelopes   []domain.AlarmQueueEnvelope
	retryBackoffs  []time.Duration
}

func (d *Dispatcher) dispatchFailureBatches(
	envelopes []domain.AlarmQueueEnvelope,
	failureMessage string,
) dispatchFailureBatches {
	batches := dispatchFailureBatches{
		retryEnvelopes: make([]domain.AlarmQueueEnvelope, 0, len(envelopes)),
		dlqEnvelopes:   make([]domain.AlarmQueueEnvelope, 0, len(envelopes)),
		retryBackoffs:  make([]time.Duration, 0, len(envelopes)),
	}
	for _, envelope := range envelopes {
		updated := envelope
		retryMetadata := &domain.AlarmQueueRetryMetadata{}
		if envelope.Retry != nil {
			*retryMetadata = *envelope.Retry
		}
		retryMetadata.Attempt = nextRetryAttempt(envelope)
		retryMetadata.LastError = failureMessage
		dispatcherRetryAttempt.Observe(float64(retryMetadata.Attempt))

		if retryMetadata.Attempt >= d.retryPolicy.MaxAttempts {
			retryMetadata.RetryAfterMS = 0
			retryMetadata.NextVisibleAt = ""
			updated.Retry = retryMetadata
			batches.dlqEnvelopes = append(batches.dlqEnvelopes, updated)
			continue
		}

		backoff := d.retryBackoffForAttempt(retryMetadata.Attempt)
		retryMetadata.RetryAfterMS = backoff.Milliseconds()
		retryMetadata.NextVisibleAt = d.now().UTC().Add(backoff).Format(time.RFC3339Nano)
		updated.Retry = retryMetadata
		batches.retryEnvelopes = append(batches.retryEnvelopes, updated)
		batches.retryBackoffs = append(batches.retryBackoffs, backoff)
	}
	return batches
}

func (d *Dispatcher) persistDispatchRetries(
	ctx context.Context,
	roomID string,
	failureKind string,
	retryEnvelopes []domain.AlarmQueueEnvelope,
	retryBackoffs []time.Duration,
) error {
	if len(retryEnvelopes) > 0 {
		if err := d.consumer.ScheduleRetry(ctx, retryEnvelopes); err != nil {
			d.logger.Warn("Dispatch failed; schedule retry failed",
				slog.String("room_id", roomID),
				slog.String("failure_kind", failureKind),
				slog.Int("retry_envelopes", len(retryEnvelopes)),
				slog.Any("error", err),
			)
			return d.preserveEnvelopesAfterPersistenceFailure(
				ctx,
				roomID,
				failureKind+"_schedule_retry_failed",
				retryEnvelopes,
				fmt.Errorf("schedule retry: %w", err),
			)
		}

		dispatcherRetryScheduled.Add(float64(len(retryEnvelopes)))
		for _, backoff := range retryBackoffs {
			dispatcherRetryBackoff.Observe(backoff.Seconds())
		}
		d.logger.Warn("Dispatch failed; scheduled durable retries",
			slog.String("room_id", roomID),
			slog.String("failure_kind", failureKind),
			slog.Int("retry_envelopes", len(retryEnvelopes)),
		)
	}
	return nil
}

func (d *Dispatcher) persistDispatchDLQ(
	ctx context.Context,
	roomID string,
	failureKind string,
	dlqEnvelopes []domain.AlarmQueueEnvelope,
) error {
	if len(dlqEnvelopes) > 0 {
		if err := d.consumer.MoveToDLQ(ctx, dlqEnvelopes); err != nil {
			d.logger.Warn("Dispatch retries exhausted; move to DLQ failed",
				slog.String("room_id", roomID),
				slog.String("failure_kind", failureKind),
				slog.Int("dlq_envelopes", len(dlqEnvelopes)),
				slog.Any("error", err),
			)
			return d.preserveEnvelopesAfterPersistenceFailure(
				ctx,
				roomID,
				failureKind+"_move_to_dlq_failed",
				dlqEnvelopes,
				fmt.Errorf("move to DLQ: %w", err),
			)
		}
		dispatcherRetryDLQMoved.WithLabelValues(failureKind + "_retry_budget_exhausted").Add(float64(len(dlqEnvelopes)))
		dispatcherRetryBudgetExhausted.Add(float64(len(dlqEnvelopes)))

		d.releaseClaimKeys(ctx, roomID, claimKeysForEnvelopes(dlqEnvelopes), failureKind+" retries exhausted")
		d.logger.Warn("Dispatch retries exhausted; moved envelopes to DLQ",
			slog.String("room_id", roomID),
			slog.String("failure_kind", failureKind),
			slog.Int("dlq_envelopes", len(dlqEnvelopes)),
		)
	}
	return nil
}

func claimKeysForEnvelopes(envelopes []domain.AlarmQueueEnvelope) []string {
	claimKeys := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		claimKeys = append(claimKeys, envelope.ClaimKeys...)
	}
	return claimKeys
}

func formatDispatchFailure(failureKind string, err error) string {
	kind := failureKind
	if kind == "" {
		kind = "dispatch"
	}
	if err == nil {
		return kind + " failed"
	}
	return fmt.Sprintf("%s failed: %v", kind, err)
}

func (d *Dispatcher) preserveEnvelopesAfterPersistenceFailure(
	ctx context.Context,
	roomID string,
	reason string,
	envelopes []domain.AlarmQueueEnvelope,
	persistErr error,
) error {
	if len(envelopes) == 0 {
		return persistErr
	}

	if err := d.consumer.Requeue(ctx, envelopes); err != nil {
		d.logger.Warn("Dispatch persistence fallback requeue failed",
			slog.String("room_id", roomID),
			slog.String("reason", reason),
			slog.Int("envelopes", len(envelopes)),
			slog.Any("error", err),
		)
		return fmt.Errorf("%w: fallback requeue: %w", persistErr, err)
	}

	d.logger.Warn("Dispatch persistence fallback requeued envelopes",
		slog.String("room_id", roomID),
		slog.String("reason", reason),
		slog.Int("envelopes", len(envelopes)),
	)
	return persistErr
}

func (d *Dispatcher) releaseClaimKeys(ctx context.Context, roomID string, claimKeys []string, reason string) {
	if len(claimKeys) == 0 {
		return
	}
	if err := d.consumer.ReleaseClaimKeys(ctx, claimKeys); err != nil {
		d.logger.Warn("Release claim keys failed",
			slog.String("room_id", roomID),
			slog.String("reason", reason),
			slog.Any("error", err),
		)
	}
}

func (d *Dispatcher) retryBackoffForAttempt(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}

	backoff := cappedExponentialBackoff(d.retryPolicy.BaseBackoff, d.retryPolicy.MaxBackoff, attempt)
	return d.applyRetryJitter(backoff)
}

func cappedExponentialBackoff(baseBackoff, maxBackoff time.Duration, attempt int) time.Duration {
	backoff := baseBackoff
	for i := 1; i < attempt && backoff < maxBackoff; i++ {
		if backoff > maxBackoff/2 {
			backoff = maxBackoff
			break
		}
		backoff *= 2
	}
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return backoff
}

func (d *Dispatcher) applyRetryJitter(backoff time.Duration) time.Duration {
	if d.retryPolicy.JitterPercent <= 0 {
		return backoff
	}

	jitterRange := d.retryPolicy.JitterPercent / 100
	factor := 1 + ((d.randFloat64()*2)-1)*jitterRange
	if factor < 0 {
		factor = 0
	}
	jittered := time.Duration(float64(backoff) * factor)
	if jittered > d.retryPolicy.MaxBackoff {
		return d.retryPolicy.MaxBackoff
	}
	return jittered
}

func nextRetryAttempt(envelope domain.AlarmQueueEnvelope) int {
	if envelope.Retry == nil || envelope.Retry.Attempt < 0 {
		return 1
	}
	return envelope.Retry.Attempt + 1
}
