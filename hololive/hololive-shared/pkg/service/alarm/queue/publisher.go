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

package queue

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const AlarmDispatchQueue = contractsalarm.DispatchQueueKey
const AlarmDispatchWakeupQueue = "alarm:dispatch:wakeup"
const alarmDispatchWakeupGuardKey = "alarm:dispatch:wakeup:guard"
const defaultPublishBatchDeliveryLimit = 1000

type PublishConfig struct {
	WakeupEnabled         bool
	MaxDeliveriesPerBatch int
}

type Publisher struct {
	cache         cache.Client
	outbox        dispatchoutbox.Writer
	publishConfig PublishConfig
	logger        *slog.Logger
	now           func() time.Time
}

type PublisherOption func(*Publisher)

func WithOutbox(repository dispatchoutbox.Writer) PublisherOption {
	return func(p *Publisher) {
		p.outbox = repository
	}
}

func WithWakeupEnabled(enabled bool) PublisherOption {
	return func(p *Publisher) {
		p.publishConfig.WakeupEnabled = enabled
	}
}

func WithMaxDeliveriesPerBatch(limit int) PublisherOption {
	return func(p *Publisher) {
		if limit > 0 {
			p.publishConfig.MaxDeliveriesPerBatch = limit
		}
	}
}

func NewPublisher(c cache.Client, logger *slog.Logger, opts ...PublisherOption) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}
	initQueueMetrics()

	publisher := &Publisher{
		cache: c,
		publishConfig: PublishConfig{
			WakeupEnabled:         true,
			MaxDeliveriesPerBatch: defaultPublishBatchDeliveryLimit,
		},
		logger: logger,
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(publisher)
	}
	if publisher.publishConfig.MaxDeliveriesPerBatch <= 0 {
		publisher.publishConfig.MaxDeliveriesPerBatch = defaultPublishBatchDeliveryLimit
	}
	return publisher
}

func (p *Publisher) Publish(ctx context.Context, notification *domain.AlarmNotification, claimKeys []string) (dispatchoutbox.PublishBatchResult, error) {
	return p.PublishBatch(ctx, []*domain.AlarmNotification{notification}, [][]string{claimKeys})
}

func (p *Publisher) PublishBatch(ctx context.Context, notifications []*domain.AlarmNotification, claimKeys [][]string) (dispatchoutbox.PublishBatchResult, error) {
	startedAt := time.Now()
	if len(notifications) == 0 {
		return dispatchoutbox.PublishBatchResult{}, nil
	}
	if len(claimKeys) > 0 && len(claimKeys) != len(notifications) {
		return dispatchoutbox.PublishBatchResult{}, fmt.Errorf("publish alarm queue batch: claim key count %d does not match notification count %d", len(claimKeys), len(notifications))
	}
	envelopes, err := p.buildPublishBatchEnvelopes(notifications, claimKeys)
	if err != nil {
		return dispatchoutbox.PublishBatchResult{}, err
	}

	result := dispatchoutbox.PublishBatchResult{RequestedDeliveries: len(envelopes)}
	defer func() {
		observeAlarmDispatchPublishBatch(time.Since(startedAt), &result)
	}()

	result, err = p.publishEnvelopes(ctx, envelopes)
	return result, err
}

func (p *Publisher) PublishDispatchBatch(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	startedAt := time.Now()
	if len(envelopes) == 0 {
		return dispatchoutbox.PublishBatchResult{}, nil
	}
	for i := range envelopes {
		if err := envelopes[i].ValidateCanonicalDispatch(); err != nil {
			return dispatchoutbox.PublishBatchResult{}, fmt.Errorf("publish alarm dispatch batch: validate envelope %d: %w", i, err)
		}
		if strings.TrimSpace(envelopes[i].EnqueuedAt) == "" {
			envelopes[i].EnqueuedAt = p.now().UTC().Format(time.RFC3339)
		}
		if envelopes[i].Version == 0 {
			envelopes[i].Version = contractsalarm.QueueEnvelopeVersionV1
		}
	}

	result := dispatchoutbox.PublishBatchResult{RequestedDeliveries: len(envelopes)}
	defer func() {
		observeAlarmDispatchPublishBatch(time.Since(startedAt), &result)
	}()

	result, err := p.publishEnvelopes(ctx, envelopes)
	return result, err
}

func (p *Publisher) buildPublishBatchEnvelopes(
	notifications []*domain.AlarmNotification,
	claimKeys [][]string,
) ([]domain.AlarmQueueEnvelope, error) {
	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(notifications))
	for i, notification := range notifications {
		envelope, err := p.buildPublishBatchEnvelope(i, notification, claimKeys)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func (p *Publisher) buildPublishBatchEnvelope(
	index int,
	notification *domain.AlarmNotification,
	claimKeys [][]string,
) (domain.AlarmQueueEnvelope, error) {
	if notification == nil {
		return domain.AlarmQueueEnvelope{}, fmt.Errorf("publish alarm queue batch: notification %d is nil", index)
	}
	if err := notification.ValidateLiveDispatchRoute(); err != nil {
		return domain.AlarmQueueEnvelope{}, fmt.Errorf("publish alarm queue batch: validate live dispatch route: %w", err)
	}
	if len(claimKeys) == 0 {
		return p.buildEnvelope(notification, nil), nil
	}
	return p.buildEnvelope(notification, claimKeys[index]), nil
}

func (p *Publisher) publishEnvelopes(
	ctx context.Context,
	envelopes []domain.AlarmQueueEnvelope,
) (dispatchoutbox.PublishBatchResult, error) {
	return p.publishPGFirstBatch(ctx, envelopes)
}

func (p *Publisher) publishPGFirstBatch(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	if p.outbox == nil {
		return dispatchoutbox.PublishBatchResult{RequestedDeliveries: len(envelopes)}, fmt.Errorf("publish alarm queue batch: pg_first requires outbox repository")
	}
	result, err := p.insertOutboxChunks(ctx, envelopes, dispatchoutbox.StatusPending)
	result.RequestedDeliveries = len(envelopes)
	if err != nil {
		return result, fmt.Errorf("publish alarm queue batch: insert pending outbox: %w", err)
	}
	if result.InsertedDeliveries > 0 && p.publishConfig.WakeupEnabled {
		p.publishWakeup(ctx)
	}
	return result, nil
}

func (p *Publisher) insertOutboxChunks(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, status dispatchoutbox.Status) (dispatchoutbox.PublishBatchResult, error) {
	var total dispatchoutbox.PublishBatchResult
	limit := p.publishConfig.MaxDeliveriesPerBatch
	if limit <= 0 {
		limit = defaultPublishBatchDeliveryLimit
	}
	for start := 0; start < len(envelopes); start += limit {
		end := min(start+limit, len(envelopes))
		result, err := p.outbox.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: envelopes[start:end], Status: status})
		if err != nil {
			return total, err
		}
		total.RequestedEvents += result.RequestedEvents
		total.InsertedEvents += result.InsertedEvents
		total.DuplicateEvents += result.DuplicateEvents
		total.HashConflictEvents += result.HashConflictEvents
		total.RequestedDeliveries += len(envelopes[start:end])
		total.ProcessedDeliveries += len(envelopes[start:end])
		total.InsertedDeliveries += result.InsertedDeliveries
		total.DuplicateDeliveries += result.DuplicateDeliveries
		total.TerminalDuplicates += result.TerminalDuplicates
	}
	return total, nil
}

func (p *Publisher) buildEnvelope(notification *domain.AlarmNotification, claimKeys []string) domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
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
