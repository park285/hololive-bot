package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/handoffcheck"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/leaseowner"
)

type Consumer struct {
	repository          Repository
	claimReleaser       ClaimKeyReleaser
	workerID            string
	lease               time.Duration
	quarantineThreshold time.Duration
	recoveryBatchSize   int
	recoveryInterval    time.Duration
	lastRecoveryAt      time.Time
	logger              *slog.Logger
	now                 func() time.Time
}

type ConsumerOption func(*Consumer)

func WithWorkerID(workerID string) ConsumerOption {
	return func(c *Consumer) {
		if workerID != "" {
			c.workerID = workerID
		}
	}
}

func WithLease(lease time.Duration) ConsumerOption {
	return func(c *Consumer) {
		if lease > 0 {
			c.lease = lease
		}
	}
}

func WithQuarantineThreshold(threshold time.Duration) ConsumerOption {
	return func(c *Consumer) {
		c.quarantineThreshold = threshold
	}
}

func WithRecoveryInterval(interval time.Duration) ConsumerOption {
	return func(c *Consumer) {
		if interval > 0 {
			c.recoveryInterval = interval
		}
	}
}

func WithRecoveryBatchSize(size int) ConsumerOption {
	return func(c *Consumer) {
		if size > 0 {
			c.recoveryBatchSize = size
		}
	}
}

func NewConsumer(repository Repository, claimReleaser ClaimKeyReleaser, logger *slog.Logger, opts ...ConsumerOption) (*Consumer, error) {
	if logger == nil {
		logger = slog.Default()
	}

	consumer := &Consumer{
		repository:        repository,
		claimReleaser:     claimReleaser,
		lease:             60 * time.Second,
		recoveryBatchSize: 100,
		recoveryInterval:  30 * time.Second,
		logger:            logger,
		now:               time.Now,
	}

	for _, opt := range opts {
		opt(consumer)
	}

	if consumer.lease < time.Millisecond {
		return nil, fmt.Errorf("new dispatch outbox consumer: lease must be at least 1ms: %s", consumer.lease)
	}

	if consumer.quarantineThreshold < consumer.lease {
		return nil, fmt.Errorf("new dispatch outbox consumer: quarantine threshold %s must be at least lease %s", consumer.quarantineThreshold, consumer.lease)
	}

	if consumer.workerID == "" {
		workerID, err := leaseowner.InstanceID("dispatcher")
		if err != nil {
			return nil, fmt.Errorf("new dispatch outbox consumer: %w", err)
		}

		consumer.workerID = workerID
	}

	return consumer, nil
}

func (c *Consumer) DrainBatch(ctx context.Context, maxItems int) ([]AlarmQueueEnvelope, error) {
	if c == nil || c.repository == nil {
		return nil, errors.New("drain outbox batch: repository is nil")
	}

	c.maybeRecover(ctx)

	records, err := c.claimDue(ctx, maxItems)
	if err != nil {
		return nil, fmt.Errorf("claim due: %w", err)
	}

	events, err := c.repository.LoadEventsByID(ctx, distinctEventIDs(records))
	if err != nil {
		return nil, c.releaseFailedBatch(ctx, records, fmt.Errorf("drain outbox batch: load events: %w", err))
	}

	out, err := c.envelopesFromRecords(ctx, records, events)
	if err != nil {
		return nil, c.releaseFailedBatch(ctx, records, fmt.Errorf("envelopes from records: %w", err))
	}

	return out, nil
}

func (c *Consumer) releaseFailedBatch(ctx context.Context, records []*Record, cause error) error {
	claims := make([]DeliveryClaim, 0, len(records))
	for _, record := range records {
		if record.Status != StatusDLQ {
			claims = append(claims, DeliveryClaim{ID: record.ID, AttemptCount: record.AttemptCount})
		}
	}

	if len(claims) == 0 {
		return cause
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := c.repository.ReleaseLeased(cleanupCtx, claims, c.workerID); err != nil {
		return errors.Join(cause, fmt.Errorf("release failed outbox batch: %w", err))
	}

	return cause
}

func (c *Consumer) claimDue(ctx context.Context, maxItems int) ([]*Record, error) {
	records, err := c.repository.ClaimDue(ctx, c.workerID, maxItems, c.lease)
	if err != nil {
		return nil, fmt.Errorf("drain outbox batch: claim due: %w", err)
	}

	observePGClaimed(len(records))

	return records, nil
}

func (c *Consumer) maybeRecover(ctx context.Context) {
	now := c.now()
	if !c.lastRecoveryAt.IsZero() && c.recoveryInterval > 0 && now.Sub(c.lastRecoveryAt) < c.recoveryInterval {
		return
	}

	c.lastRecoveryAt = now

	recoveredLeased, leasedErr := c.repository.RecoverExpiredLeased(ctx, c.recoveryBatchSize)
	if leasedErr != nil {
		observeRecoveryFailure(recoveryTypeLeased)
		c.logger.Warn("Recover expired leased dispatch rows failed", slog.Any("error", leasedErr))
	} else {
		observeRecoveryRows(recoveryTypeLeased, recoveredLeased)
	}

	recoveredSending, sendingErr := c.repository.QuarantineStaleSending(ctx, c.quarantineThreshold, c.recoveryBatchSize)
	if sendingErr != nil {
		observeRecoveryFailure(recoveryTypeSending)
		c.logger.Warn("Quarantine stale sending dispatch rows failed", slog.Any("error", sendingErr))
	} else {
		observeRecoveryRows(recoveryTypeSending, recoveredSending)
	}

	if leasedErr == nil && sendingErr == nil {
		observeRecoverySuccess(now)
	}
}

func (c *Consumer) MarkSending(ctx context.Context, envelopes []AlarmQueueEnvelope) error {
	claims := claimsFromEnvelopes(envelopes)
	if len(claims) == 0 || len(envelopes) == 0 {
		return nil
	}

	unit := &envelopes[0]
	if err := c.repository.BeginSendRequest(ctx, unit.SendUnitID, claims, c.workerID, unit.ClientRequestID, c.lease); err != nil {
		observePGMarkSendingFailure()

		return fmt.Errorf("begin immutable alarm send: %w", err)
	}

	return nil
}

func (c *Consumer) MarkDispatched(ctx context.Context, envelopes []AlarmQueueEnvelope, irisRequestID string, handoffHorizon time.Duration) error {
	claims := claimsFromEnvelopes(envelopes)
	if len(claims) == 0 {
		return nil
	}

	firstCheck := min(handoffcheck.FirstCheckDelay, handoffHorizon)
	if err := c.repository.MarkSent(ctx, envelopes[0].SendUnitID, claims, c.workerID, irisRequestID, firstCheck); err != nil {
		observePGMarkSentFailure()

		return fmt.Errorf("mark sent: %w", err)
	}

	return nil
}

func (c *Consumer) ReleaseLeased(ctx context.Context, envelopes []AlarmQueueEnvelope) error {
	if err := c.repository.ReleaseLeased(ctx, claimsFromEnvelopes(envelopes), c.workerID); err != nil {
		return fmt.Errorf("release leased: %w", err)
	}

	return nil
}

func (c *Consumer) RouteFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []AlarmQueueEnvelope) error {
	updates, retryCount, dlqCount := failureUpdatesFromEnvelopes(retryEnvelopes, dlqEnvelopes)
	if err := observeRoutedFailures(c.repository.RouteFailures(ctx, updates, c.workerID), updates, retryCount, dlqCount); err != nil {
		return fmt.Errorf("observe routed failures: %w", err)
	}

	return nil
}

func (c *Consumer) RouteSendingFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []AlarmQueueEnvelope) error {
	updates, retryCount, dlqCount := failureUpdatesFromEnvelopes(retryEnvelopes, dlqEnvelopes)
	if err := observeRoutedFailures(c.repository.RouteSendingFailures(ctx, updates, c.workerID), updates, retryCount, dlqCount); err != nil {
		return fmt.Errorf("observe routed failures: %w", err)
	}

	return nil
}

func (c *Consumer) RequeuePreSend(ctx context.Context, envelopes []AlarmQueueEnvelope) error {
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

	if miss, ok := errors.AsType[*sendfence.MissError](err); ok {
		retryApplied, dlqApplied := appliedFailureCounts(updates, miss.UnappliedDeliveryIDs())
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

func failureUpdatesFromEnvelopes(retryEnvelopes, dlqEnvelopes []AlarmQueueEnvelope) (updates []FailureUpdate, retryCount, dlqCount int) {
	updates = make([]FailureUpdate, 0, len(retryEnvelopes)+len(dlqEnvelopes))
	updates = appendFailureUpdates(updates, retryEnvelopes, StatusRetry)
	retryCount = len(updates)
	updates = appendFailureUpdates(updates, dlqEnvelopes, StatusDLQ)

	return updates, retryCount, len(updates) - retryCount
}

func appendFailureUpdates(updates []FailureUpdate, envelopes []AlarmQueueEnvelope, target Status) []FailureUpdate {
	for i := range envelopes {
		update, ok := failureUpdateFromEnvelope(&envelopes[i], target)
		if !ok {
			continue
		}

		updates = append(updates, update)
	}

	return updates
}

func failureUpdateFromEnvelope(envelope *AlarmQueueEnvelope, target Status) (FailureUpdate, bool) {
	if envelope.DispatchOutboxID <= 0 {
		return FailureUpdate{}, false
	}

	update := FailureUpdate{ID: envelope.DispatchOutboxID, TargetStatus: target}
	if envelope.Retry == nil {
		return update, true
	}

	update.AttemptCount = envelope.Retry.Attempt
	update.RetryAfter = time.Duration(envelope.Retry.RetryAfterMS) * time.Millisecond
	update.Error = sanitizeStoredError(envelope.Retry.LastError)
	update.ErrorCode = envelope.Retry.LastErrorCode

	return update, true
}

func (c *Consumer) Requeue(ctx context.Context, envelopes []AlarmQueueEnvelope) error {
	if err := c.RouteFailures(ctx, envelopes, nil); err != nil {
		return fmt.Errorf("route failures: %w", err)
	}

	return nil
}

func (c *Consumer) Quarantine(ctx context.Context, envelopes []AlarmQueueEnvelope, cause error) error {
	message, code := storedErrorFromCause(cause)
	updates := make([]TerminalUpdate, 0, len(envelopes))

	for i := range envelopes {
		envelope := &envelopes[i]
		if envelope.DispatchOutboxID > 0 {
			updates = append(updates, TerminalUpdate{ID: envelope.DispatchOutboxID, AttemptCount: envelopeAttempt(envelope), Error: message, ErrorCode: code})
		}
	}

	if err := c.repository.Quarantine(ctx, updates, c.workerID); err != nil {
		return fmt.Errorf("quarantine: %w", err)
	}

	observePGQuarantined(len(updates))

	return nil
}
