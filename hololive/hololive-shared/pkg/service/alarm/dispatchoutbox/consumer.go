package dispatchoutbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	json "github.com/park285/hololive-bot/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Consumer struct {
	repository              Repository
	workerID          string
	lease             time.Duration
	recoveryBatchSize int
	recoveryInterval  time.Duration
	lastRecoveryAt    time.Time
	logger            *slog.Logger
	now               func() time.Time
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
	host, _ := os.Hostname()
	consumer := &Consumer{
		repository:              repository,
		workerID:          "dispatcher-" + host,
		lease:             60 * time.Second,
		recoveryBatchSize: 100,
		recoveryInterval:  30 * time.Second,
		logger:            logger,
		now:               time.Now,
	}
	for _, opt := range opts {
		opt(consumer)
	}
	return consumer
}

func (c *Consumer) DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
	if c == nil || c.repository == nil {
		return nil, fmt.Errorf("drain outbox batch: repository is nil")
	}
	if err := c.maybeRecover(ctx); err != nil {
		return nil, err
	}
	records, err := c.claimDue(ctx, maxItems)
	if err != nil {
		return nil, err
	}
	events, err := c.repository.LoadEventsByID(ctx, distinctEventIDs(records))
	if err != nil {
		return nil, fmt.Errorf("drain outbox batch: load events: %w", err)
	}
	return c.envelopesFromRecords(ctx, records, events)
}

func (c *Consumer) envelopesFromRecords(ctx context.Context, records []*Record, events map[int64]EventRecord) ([]domain.AlarmQueueEnvelope, error) {
	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(records))
	for _, record := range records {
		envelope, ok, err := c.envelopeFromRecord(ctx, record, events)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func (c *Consumer) envelopeFromRecord(ctx context.Context, record *Record, events map[int64]EventRecord) (domain.AlarmQueueEnvelope, bool, error) {
	payload, ok, err := c.payloadForRecord(ctx, record, events)
	if err != nil || !ok {
		return domain.AlarmQueueEnvelope{}, false, err
	}
	envelope, ok, err := c.decodeEnvelopePayload(ctx, record, payload)
	if err != nil || !ok {
		return domain.AlarmQueueEnvelope{}, false, err
	}
	ok, err = c.rehydrateEnvelope(ctx, record, &envelope)
	if err != nil || !ok {
		return domain.AlarmQueueEnvelope{}, false, err
	}
	attachRecordMetadata(&envelope, record)
	return envelope, true, nil
}

func (c *Consumer) decodeEnvelopePayload(ctx context.Context, record *Record, payload []byte) (domain.AlarmQueueEnvelope, bool, error) {
	var envelope domain.AlarmQueueEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		if err := c.moveRecordToDLQ(ctx, record.ID, fmt.Sprintf("invalid payload: %v", err), "move invalid payload to dlq"); err != nil {
			return domain.AlarmQueueEnvelope{}, false, err
		}
		return domain.AlarmQueueEnvelope{}, false, nil
	}
	return envelope, true, nil
}

func (c *Consumer) rehydrateEnvelope(ctx context.Context, record *Record, envelope *domain.AlarmQueueEnvelope) (bool, error) {
	if err := rehydrateDeliveryContext(envelope, record); err != nil {
		if err := c.moveRecordToDLQ(ctx, record.ID, fmt.Sprintf("invalid delivery context: %v", err), "move invalid delivery context to dlq"); err != nil {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

func attachRecordMetadata(envelope *domain.AlarmQueueEnvelope, record *Record) {
	envelope.DispatchOutboxID = record.ID
	envelope.ClaimKeys = record.ClaimKeys
	if record.AttemptCount > 0 {
		envelope.Retry = &domain.AlarmQueueRetryMetadata{
			Attempt:   record.AttemptCount,
			LastError: record.Error,
		}
	}
}

func (c *Consumer) payloadForRecord(ctx context.Context, record *Record, events map[int64]EventRecord) ([]byte, bool, error) {
	if record.EventID <= 0 {
		return record.Payload, true, nil
	}
	event, ok := events[record.EventID]
	if ok {
		return event.Payload, true, nil
	}
	if err := c.moveRecordToDLQ(ctx, record.ID, "missing event payload", "move missing event to dlq"); err != nil {
		return nil, false, err
	}
	return nil, false, nil
}

func (c *Consumer) claimDue(ctx context.Context, maxItems int) ([]*Record, error) {
	records, err := c.repository.ClaimDue(ctx, c.workerID, maxItems, c.lease)
	if err != nil {
		return nil, fmt.Errorf("drain outbox batch: claim due: %w", err)
	}
	observePGClaimed(len(records))
	return records, nil
}

func (c *Consumer) moveRecordToDLQ(ctx context.Context, id int64, terminalError string, action string) error {
	if err := c.repository.MoveToDLQ(ctx, []TerminalUpdate{{ID: id, Error: terminalError}}, c.workerID); err != nil {
		return fmt.Errorf("drain outbox batch: %s: %w", action, err)
	}
	observePGDLQ(1)
	return nil
}

func (c *Consumer) maybeRecover(ctx context.Context) error {
	now := c.now()
	if !c.lastRecoveryAt.IsZero() && c.recoveryInterval > 0 && now.Sub(c.lastRecoveryAt) < c.recoveryInterval {
		return nil
	}
	c.lastRecoveryAt = now
	recoveredLeased, leasedErr := c.repository.RecoverExpiredLeased(ctx, c.recoveryBatchSize)
	if leasedErr != nil {
		observeRecoveryFailure(recoveryTypeLeased)
		c.logger.Warn("Recover expired leased dispatch rows failed", slog.Any("error", leasedErr))
	} else {
		observeRecoveryRows(recoveryTypeLeased, recoveredLeased)
	}
	recoveredSending, sendingErr := c.repository.QuarantineStaleSending(ctx, c.lease, c.recoveryBatchSize)
	if sendingErr != nil {
		observeRecoveryFailure(recoveryTypeSending)
		c.logger.Warn("Quarantine stale sending dispatch rows failed", slog.Any("error", sendingErr))
	} else {
		observeRecoveryRows(recoveryTypeSending, recoveredSending)
	}
	if leasedErr == nil && sendingErr == nil {
		observeRecoverySuccess(now)
	}
	return nil
}

func (c *Consumer) MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	ids := idsFromEnvelopes(envelopes)
	if len(ids) == 0 {
		return nil
	}
	if err := c.repository.MarkSending(ctx, ids, c.workerID, c.lease); err != nil {
		observePGMarkSendingFailure()
		return err
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
		return err
	}
	return nil
}

func (c *Consumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	return nil
}

func (c *Consumer) ScheduleRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	updates := make([]RetryUpdate, 0, len(envelopes))
	now := time.Now().UTC()
	for _, envelope := range envelopes {
		update, ok := retryUpdateFromEnvelope(envelope, now)
		if !ok {
			continue
		}
		updates = append(updates, update)
	}
	if err := c.repository.ScheduleRetry(ctx, updates, c.workerID); err != nil {
		return err
	}
	observePGRetryScheduled(len(updates))
	return nil
}

func retryUpdateFromEnvelope(envelope domain.AlarmQueueEnvelope, now time.Time) (RetryUpdate, bool) {
	if envelope.DispatchOutboxID <= 0 {
		return RetryUpdate{}, false
	}
	update := RetryUpdate{ID: envelope.DispatchOutboxID, NextAttemptAt: now}
	if envelope.Retry == nil {
		return update, true
	}
	update.AttemptCount = envelope.Retry.Attempt
	update.Error = envelope.Retry.LastError
	if parsed, err := time.Parse(time.RFC3339Nano, envelope.Retry.NextVisibleAt); err == nil {
		update.NextAttemptAt = parsed.UTC()
	}
	return update, true
}

func (c *Consumer) MoveToDLQ(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	updates := make([]TerminalUpdate, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.DispatchOutboxID <= 0 {
			continue
		}
		update := TerminalUpdate{ID: envelope.DispatchOutboxID}
		if envelope.Retry != nil {
			update.Error = envelope.Retry.LastError
		}
		updates = append(updates, update)
	}
	if err := c.repository.MoveToDLQ(ctx, updates, c.workerID); err != nil {
		return err
	}
	observePGDLQ(len(updates))
	return nil
}

func (c *Consumer) Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	return c.ScheduleRetry(ctx, envelopes)
}

func (c *Consumer) Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, reason string) error {
	updates := make([]TerminalUpdate, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.DispatchOutboxID > 0 {
			updates = append(updates, TerminalUpdate{ID: envelope.DispatchOutboxID, Error: reason})
		}
	}
	if err := c.repository.Quarantine(ctx, updates, c.workerID); err != nil {
		return err
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
	var context deliveryContext
	if err := json.Unmarshal(record.DeliveryContext, &context); err != nil {
		return err
	}
	envelope.Notification.Users = context.Users
	return nil
}
