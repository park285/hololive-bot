package dispatchoutbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Consumer struct {
	repo              Repository
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

func NewConsumer(repo Repository, logger *slog.Logger, opts ...ConsumerOption) *Consumer {
	if logger == nil {
		logger = slog.Default()
	}
	host, _ := os.Hostname()
	consumer := &Consumer{
		repo:              repo,
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
	if c == nil || c.repo == nil {
		return nil, fmt.Errorf("drain outbox batch: repository is nil")
	}
	if err := c.maybeRecover(ctx); err != nil {
		return nil, err
	}
	records, err := c.repo.ClaimDue(ctx, c.workerID, maxItems, c.lease)
	if err != nil {
		return nil, fmt.Errorf("drain outbox batch: claim due: %w", err)
	}
	events, err := c.repo.LoadEventsByID(ctx, distinctEventIDs(records))
	if err != nil {
		return nil, fmt.Errorf("drain outbox batch: load events: %w", err)
	}
	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(records))
	for _, record := range records {
		var envelope domain.AlarmQueueEnvelope
		payload := record.Payload
		if record.EventID > 0 {
			event, ok := events[record.EventID]
			if !ok {
				if moveErr := c.repo.MoveToDLQ(ctx, []TerminalUpdate{{ID: record.ID, Error: "missing event payload"}}, c.workerID); moveErr != nil {
					return nil, fmt.Errorf("drain outbox batch: move missing event to dlq: %w", moveErr)
				}
				continue
			}
			payload = event.Payload
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			if moveErr := c.repo.MoveToDLQ(ctx, []TerminalUpdate{{ID: record.ID, Error: fmt.Sprintf("invalid payload: %v", err)}}, c.workerID); moveErr != nil {
				return nil, fmt.Errorf("drain outbox batch: move invalid payload to dlq: %w", moveErr)
			}
			continue
		}
		if err := rehydrateDeliveryContext(&envelope, record); err != nil {
			if moveErr := c.repo.MoveToDLQ(ctx, []TerminalUpdate{{ID: record.ID, Error: fmt.Sprintf("invalid delivery context: %v", err)}}, c.workerID); moveErr != nil {
				return nil, fmt.Errorf("drain outbox batch: move invalid delivery context to dlq: %w", moveErr)
			}
			continue
		}
		envelope.DispatchOutboxID = record.ID
		envelope.ClaimKeys = record.ClaimKeys
		if record.AttemptCount > 0 {
			envelope.Retry = &domain.AlarmQueueRetryMetadata{
				Attempt:   record.AttemptCount,
				LastError: record.Error,
			}
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func (c *Consumer) maybeRecover(ctx context.Context) error {
	now := c.now()
	if !c.lastRecoveryAt.IsZero() && c.recoveryInterval > 0 && now.Sub(c.lastRecoveryAt) < c.recoveryInterval {
		return nil
	}
	c.lastRecoveryAt = now
	recoveredLeased, leasedErr := c.repo.RecoverExpiredLeased(ctx, c.recoveryBatchSize)
	if leasedErr != nil {
		observeRecoveryFailure(recoveryTypeLeased)
		c.logger.Warn("Recover expired leased dispatch rows failed", slog.Any("error", leasedErr))
	} else {
		observeRecoveryRows(recoveryTypeLeased, recoveredLeased)
	}
	recoveredSending, sendingErr := c.repo.QuarantineStaleSending(ctx, c.lease, c.recoveryBatchSize)
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
	return c.repo.MarkSending(ctx, ids, c.workerID, c.lease)
}

func (c *Consumer) MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	ids := idsFromEnvelopes(envelopes)
	if len(ids) == 0 {
		return nil
	}
	return c.repo.MarkSent(ctx, ids, c.workerID)
}

func (c *Consumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	return nil
}

func (c *Consumer) ScheduleRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	updates := make([]RetryUpdate, 0, len(envelopes))
	now := time.Now().UTC()
	for _, envelope := range envelopes {
		if envelope.DispatchOutboxID <= 0 {
			continue
		}
		update := RetryUpdate{ID: envelope.DispatchOutboxID, NextAttemptAt: now}
		if envelope.Retry != nil {
			update.AttemptCount = envelope.Retry.Attempt
			update.Error = envelope.Retry.LastError
			if parsed, err := time.Parse(time.RFC3339Nano, envelope.Retry.NextVisibleAt); err == nil {
				update.NextAttemptAt = parsed.UTC()
			}
		}
		updates = append(updates, update)
	}
	return c.repo.ScheduleRetry(ctx, updates, c.workerID)
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
	return c.repo.MoveToDLQ(ctx, updates, c.workerID)
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
	return c.repo.Quarantine(ctx, updates, c.workerID)
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
