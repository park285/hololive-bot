package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	AlarmDispatchWakeupQueue         = "alarm:dispatch:wakeup"
	alarmDispatchWakeupGuardKey      = "alarm:dispatch:wakeup:guard"
	alarmDispatchWakeupSignalTimeout = 500 * time.Millisecond
)

type PublishConfig struct {
	WakeupEnabled         bool
	MaxDeliveriesPerBatch int
}

type WakeupCache interface {
	cache.KeyValueWriter
	cache.KeyValueLifecycle
	cache.LowLevelCache
}

type Publisher struct {
	cache         WakeupCache
	outbox        dispatchoutbox.Writer
	publishConfig PublishConfig
	logger        *slog.Logger
	now           func() time.Time
}

func NewPublisher(c WakeupCache, outbox dispatchoutbox.Writer, config PublishConfig, logger *slog.Logger) (*Publisher, error) {
	if outbox == nil {
		return nil, errors.New("new alarm publisher: outbox repository is required")
	}

	if config.MaxDeliveriesPerBatch <= 0 {
		return nil, fmt.Errorf("new alarm publisher: max deliveries per batch must be positive, got %d", config.MaxDeliveriesPerBatch)
	}

	if logger == nil {
		logger = slog.Default()
	}

	queueMetrics()

	return &Publisher{
		cache:         c,
		outbox:        outbox,
		publishConfig: config,
		logger:        logger,
		now:           time.Now,
	}, nil
}

func (p *Publisher) PublishBatch(ctx context.Context, notifications []*domain.AlarmNotification, claimKeys [][]string) (dispatchoutbox.PublishBatchResult, error) {
	startedAt := time.Now()

	if len(notifications) == 0 {
		return dispatchoutbox.PublishBatchResult{}, nil
	}

	if len(claimKeys) != len(notifications) {
		return dispatchoutbox.PublishBatchResult{}, fmt.Errorf("publish alarm queue batch: claim key count %d does not match notification count %d", len(claimKeys), len(notifications))
	}

	envelopes, err := p.buildPublishBatchEnvelopes(notifications, claimKeys)
	if err != nil {
		return dispatchoutbox.PublishBatchResult{}, fmt.Errorf("build publish batch envelopes: %w", err)
	}

	result := dispatchoutbox.PublishBatchResult{RequestedDeliveries: len(envelopes)}

	defer func() {
		observeAlarmDispatchPublishBatch(time.Since(startedAt), &result)
	}()

	result, err = p.publishEnvelopes(ctx, envelopes)
	if err != nil {
		return result, fmt.Errorf("publish envelopes: %w", err)
	}

	return result, nil
}

func (p *Publisher) PublishDispatchBatch(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	startedAt := time.Now()

	if len(envelopes) == 0 {
		return dispatchoutbox.PublishBatchResult{}, nil
	}

	if err := p.prepareDispatchEnvelopes(envelopes); err != nil {
		return dispatchoutbox.PublishBatchResult{}, fmt.Errorf("prepare dispatch envelopes: %w", err)
	}

	result := dispatchoutbox.PublishBatchResult{RequestedDeliveries: len(envelopes)}

	defer func() {
		observeAlarmDispatchPublishBatch(time.Since(startedAt), &result)
	}()

	result, err := p.publishEnvelopes(ctx, envelopes)
	if err != nil {
		return result, fmt.Errorf("publish dispatch batch: %w", err)
	}

	return result, nil
}

func (p *Publisher) prepareDispatchEnvelopes(envelopes []dispatchoutbox.AlarmQueueEnvelope) error {
	for i := range envelopes {
		if err := envelopes[i].ValidateCanonicalDispatch(); err != nil {
			return fmt.Errorf("publish alarm dispatch batch: validate envelope %d: %w", i, err)
		}

		if strings.TrimSpace(envelopes[i].EnqueuedAt) == "" {
			envelopes[i].EnqueuedAt = p.now().UTC().Format(time.RFC3339)
		}

		if envelopes[i].Version != contractsalarm.QueueEnvelopeVersionV1 {
			return fmt.Errorf("publish alarm dispatch batch: envelope %d: envelope version %d is unsupported (want %d)",
				i, envelopes[i].Version, contractsalarm.QueueEnvelopeVersionV1)
		}
	}

	return nil
}

func (p *Publisher) buildPublishBatchEnvelopes(
	notifications []*domain.AlarmNotification,
	claimKeys [][]string,
) ([]dispatchoutbox.AlarmQueueEnvelope, error) {
	envelopes := make([]dispatchoutbox.AlarmQueueEnvelope, 0, len(notifications))
	for i, notification := range notifications {
		envelope, err := p.buildPublishBatchEnvelope(i, notification, claimKeys)
		if err != nil {
			return nil, fmt.Errorf("build publish batch envelope: %w", err)
		}

		envelopes = append(envelopes, envelope)
	}

	return envelopes, nil
}

func (p *Publisher) buildPublishBatchEnvelope(
	index int,
	notification *domain.AlarmNotification,
	claimKeys [][]string,
) (dispatchoutbox.AlarmQueueEnvelope, error) {
	if notification == nil {
		return dispatchoutbox.AlarmQueueEnvelope{}, fmt.Errorf("publish alarm queue batch: notification %d is nil", index)
	}

	if err := notification.ValidateLiveDispatchRoute(); err != nil {
		return dispatchoutbox.AlarmQueueEnvelope{}, fmt.Errorf("publish alarm queue batch: validate live dispatch route: %w", err)
	}

	return p.buildEnvelope(notification, claimKeys[index]), nil
}

func (p *Publisher) publishEnvelopes(
	ctx context.Context,
	envelopes []dispatchoutbox.AlarmQueueEnvelope,
) (dispatchoutbox.PublishBatchResult, error) {
	result, err := p.insertOutboxChunks(ctx, envelopes)

	result.RequestedDeliveries = len(envelopes)

	if err != nil {
		return result, fmt.Errorf("publish alarm queue batch: insert pending outbox: %w", err)
	}

	if result.InsertedDeliveries > 0 && p.publishConfig.WakeupEnabled {
		p.publishWakeup(ctx)
	}

	return result, nil
}

func (p *Publisher) insertOutboxChunks(ctx context.Context, envelopes []dispatchoutbox.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	var total dispatchoutbox.PublishBatchResult

	limit := p.publishConfig.MaxDeliveriesPerBatch

	for start := 0; start < len(envelopes); start += limit {
		end := min(start+limit, len(envelopes))

		result, err := p.outbox.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: envelopes[start:end]})
		for _, receipt := range result.Receipts {
			receipt.Ordinal += start

			total.Receipts = append(total.Receipts, receipt)
		}

		total.RequestedEvents += result.RequestedEvents
		total.InsertedEvents += result.InsertedEvents
		total.DuplicateEvents += result.DuplicateEvents
		total.HashConflictEvents += result.HashConflictEvents
		total.RequestedDeliveries += len(envelopes[start:end])
		total.ProcessedDeliveries += len(result.Receipts)
		total.InsertedDeliveries += result.InsertedDeliveries
		total.DuplicateDeliveries += result.DuplicateDeliveries
		total.TerminalDuplicates += result.TerminalDuplicates

		if err != nil {
			return total, fmt.Errorf("insert batch: %w", err)
		}
	}

	return total, nil
}

func (p *Publisher) buildEnvelope(notification *domain.AlarmNotification, claimKeys []string) dispatchoutbox.AlarmQueueEnvelope {
	return dispatchoutbox.AlarmQueueEnvelope{
		Notification: *notification,
		ClaimKeys:    claimKeys,
		EnqueuedAt:   p.now().UTC().Format(time.RFC3339),
		Version:      contractsalarm.QueueEnvelopeVersionV1,
	}
}

func (p *Publisher) publishWakeup(ctx context.Context) {
	if p.cache == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alarmDispatchWakeupSignalTimeout)
	defer cancel()

	acquired, err := p.cache.SetNX(ctx, alarmDispatchWakeupGuardKey, "1", 3*time.Second)
	if err != nil {
		observeAlarmDispatchWakeupFailed()
		p.logger.Warn("Alarm outbox wakeup guard failed", slog.Any("error", err))

		return
	}

	if !acquired {
		observeAlarmDispatchWakeupSuppressed()

		return
	}

	cmd := p.cache.B().Lpush().Key(AlarmDispatchWakeupQueue).Element("1").Build()
	results := p.cache.DoMulti(ctx, cmd)

	if len(results) != 1 {
		observeAlarmDispatchWakeupFailed()
		p.logger.Warn("Alarm outbox wakeup failed", slog.Int("result_count", len(results)))

		return
	}

	if err := results[0].Error(); err != nil {
		observeAlarmDispatchWakeupFailed()
		p.logger.Warn("Alarm outbox wakeup failed", slog.Any("error", err))

		return
	}

	observeAlarmDispatchWakeupSent()

	if err := p.cache.Expire(ctx, AlarmDispatchWakeupQueue, 5*time.Second); err != nil {
		observeAlarmDispatchWakeupExpireFailed()
		p.logger.Warn("Alarm outbox wakeup expire failed", slog.Any("error", err))
	}
}
