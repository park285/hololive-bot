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
	"github.com/kapu/hololive-shared/pkg/domain"
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

// WithQuarantineThreshold는 stale sending 격리 기준이다. 값을 바꾸지 않고 그대로 두므로 생략·0·음수는 NewConsumer가
// lease 미만 오류로 거부한다.
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

// NewConsumer는 WithWorkerID가 없으면 "dispatcher:hostname:pid"를 lease owner로 쓴다. 호스트 이름을 얻지 못하면
// 다른 이름으로 바꾸지 않고 생성 오류다. SQL이 lease를 밀리초 정수로 받으므로 lease가 1ms 미만이어도 생성 오류다.
// 격리 기준은 lease 이상이어야 하며, 생략·0·음수를 포함해 lease보다 작으면 기본값으로 바꾸지 않고 생성 오류다.
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
	claims := make([]DeliveryClaim, 0, len(records))
	for _, record := range records {
		if record.Status != StatusDLQ {
			claims = append(claims, DeliveryClaim{ID: record.ID, AttemptCount: record.AttemptCount})
		}
	}

	if len(claims) == 0 {
		return cause
	}

	// send-unit의 일부만 반환하면 안 되므로 복원 전후의 미발송 claim을 함께 반환합니다.
	// 확정 여부가 불명확한 전이는 저장소의 leased·소유자·attempt fence로 보호합니다.
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

// MarkSending은 그룹 전체 send unit을 고정 요청으로 발송 상태에 넣는다. 미스는 *sendfence.MissError로 돌려준다.
func (c *Consumer) MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
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

// MarkDispatched는 그룹 전체 send unit의 Iris admission을 sent로 정산하고 Iris requestId의 첫 handoff 확인을
// min(handoffcheck.FirstCheckDelay, handoffHorizon) 뒤로 예약한다. 지평 handoffHorizon은 handoff reconciler와 같은
// alarm_dispatch executor attempt_timeout이라 첫 확인도 admission + 지평을 넘지 않는다. 미스는
// *sendfence.MissError로 돌려준다.
func (c *Consumer) MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, irisRequestID string, handoffHorizon time.Duration) error {
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

// ReleaseLeased는 발송 전 claim을 같은 attempt로 반환해 다음 claim이 send unit 전원을 다시 모으게 한다.
func (c *Consumer) ReleaseLeased(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if err := c.repository.ReleaseLeased(ctx, claimsFromEnvelopes(envelopes), c.workerID); err != nil {
		return fmt.Errorf("release leased: %w", err)
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

func failureUpdatesFromEnvelopes(retryEnvelopes, dlqEnvelopes []domain.AlarmQueueEnvelope) (updates []FailureUpdate, retryCount, dlqCount int) {
	updates = make([]FailureUpdate, 0, len(retryEnvelopes)+len(dlqEnvelopes))
	updates = appendFailureUpdates(updates, retryEnvelopes, StatusRetry)
	retryCount = len(updates)
	updates = appendFailureUpdates(updates, dlqEnvelopes, StatusDLQ)

	return updates, retryCount, len(updates) - retryCount
}

func appendFailureUpdates(updates []FailureUpdate, envelopes []domain.AlarmQueueEnvelope, target Status) []FailureUpdate {
	for i := range envelopes {
		update, ok := failureUpdateFromEnvelope(&envelopes[i], target)
		if !ok {
			continue
		}

		updates = append(updates, update)
	}

	return updates
}

// failureUpdateFromEnvelope은 runner가 정한 attempt·오류·지연을 옮긴다. 다음 시도 시각은 SQL이 잠금 뒤 채취한 clock에
// RetryAfter를 더해 정하므로 한 문장에서 정산되는 멤버는 같은 기준 시각을 쓴다.
func failureUpdateFromEnvelope(envelope *domain.AlarmQueueEnvelope, target Status) (FailureUpdate, bool) {
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
			updates = append(updates, TerminalUpdate{ID: envelope.DispatchOutboxID, AttemptCount: envelopeAttempt(envelope), Error: message, ErrorCode: code})
		}
	}

	if err := c.repository.Quarantine(ctx, updates, c.workerID); err != nil {
		return fmt.Errorf("quarantine: %w", err)
	}

	observePGQuarantined(len(updates))

	return nil
}
