package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
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
		if threshold > 0 {
			c.quarantineThreshold = threshold
		}
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

func NewConsumer(repository Repository, logger *slog.Logger, opts ...ConsumerOption) *Consumer {
	if logger == nil {
		logger = slog.Default()
	}

	consumer := &Consumer{
		repository:        repository,
		workerID:          util.InstanceID("dispatcher"),
		lease:             60 * time.Second,
		recoveryBatchSize: 100,
		recoveryInterval:  30 * time.Second,
		logger:            logger,
		now:               time.Now,
	}

	for _, opt := range opts {
		opt(consumer)
	}

	// 그룹 발송(karing 최대 13회 순차 HTTP)이 lease를 초과할 수 있어, threshold가
	// lease와 같으면 진행 중인 발송을 quarantine으로 회수해 버린다.
	if consumer.quarantineThreshold <= 0 {
		consumer.quarantineThreshold = 3 * consumer.lease
	}

	if consumer.quarantineThreshold < consumer.lease {
		consumer.quarantineThreshold = consumer.lease
	}

	return consumer
}

// DrainBatch는 만료 claim을 복구하고 최대 maxItems개 delivery를 claim해 발송 입력을 복원합니다.
// 복원 불가능한 delivery는 worker 소유권을 검증해 DLQ로 확정한 뒤 해당 dedup 키를 해제합니다.
// 복원 실패 시 부분 발송 입력을 반환하지 않고, 확정된 DLQ를 제외한 배치의 lease를
// 요청 취소와 독립된 최대 5초의 정리로 반환합니다. 정리 오류는 원인 오류와 함께 반환하며
// attempt, send-unit, 미발송 dedup 키와 이미 확정된 DLQ 전이는 유지합니다.
func (c *Consumer) DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
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
	ids := make([]int64, 0, len(records))
	for _, record := range records {
		if record.Status != StatusDLQ {
			ids = append(ids, record.ID)
		}
	}

	if len(ids) == 0 {
		return cause
	}

	// send-unit의 일부만 반환하면 안 되므로 복원 전후의 미발송 claim을 함께 반환합니다.
	// 확정 여부가 불명확한 전이는 저장소의 leased/worker fence로 보호합니다.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := c.repository.ReleaseLeased(cleanupCtx, ids, c.workerID); err != nil {
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

func (c *Consumer) MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	ids := idsFromEnvelopes(envelopes)
	if len(ids) == 0 {
		return nil
	}

	if err := c.repository.MarkSending(ctx, ids, c.workerID, c.lease); err != nil {
		observePGMarkSendingFailure()

		return fmt.Errorf("mark sending: %w", err)
	}

	return nil
}

func (c *Consumer) MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	ids := idsFromEnvelopes(envelopes)
	if len(ids) == 0 {
		return nil
	}

	if err := c.repository.MarkSent(ctx, ids, c.workerID); err != nil {
		observePGMarkSentFailure()

		return fmt.Errorf("mark sent: %w", err)
	}

	return nil
}

func (c *Consumer) RouteFailures(ctx context.Context, retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) error {
	updates, retryCount, dlqCount := failureUpdatesFromEnvelopes(retryEnvelopes, dlqEnvelopes)
	if err := observeRoutedFailures(c.repository.RouteFailures(ctx, updates, c.workerID), updates, retryCount, dlqCount); err != nil {
		return fmt.Errorf("observe routed failures: %w", err)
	}

	return nil
}

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
