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
	"time"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

const AlarmDispatchQueue = contractsalarm.DispatchQueueKey
const AlarmDispatchWakeupQueue = "alarm:dispatch:wakeup"
const alarmDispatchWakeupGuardKey = "alarm:dispatch:wakeup:guard"
const defaultPublishBatchDeliveryLimit = 1000

type PublishMode string

const (
	PublishModeValkeyOnly PublishMode = "valkey_only"
	PublishModeShadow     PublishMode = "shadow"
	PublishModePGFirst    PublishMode = "pg_first"
)

type PublishConfig struct {
	Mode                  PublishMode
	ShadowFatal           bool
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

func WithOutbox(repo dispatchoutbox.Writer) PublisherOption {
	return func(p *Publisher) {
		p.outbox = repo
	}
}

func WithPublishMode(mode PublishMode) PublisherOption {
	return func(p *Publisher) {
		p.publishConfig.Mode = normalizePublishMode(mode)
	}
}

func WithShadowFatal(enabled bool) PublisherOption {
	return func(p *Publisher) {
		p.publishConfig.ShadowFatal = enabled
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
			Mode:                  PublishModeValkeyOnly,
			WakeupEnabled:         true,
			MaxDeliveriesPerBatch: defaultPublishBatchDeliveryLimit,
		},
		logger: logger,
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(publisher)
	}
	publisher.publishConfig.Mode = normalizePublishMode(publisher.publishConfig.Mode)
	if publisher.publishConfig.MaxDeliveriesPerBatch <= 0 {
		publisher.publishConfig.MaxDeliveriesPerBatch = defaultPublishBatchDeliveryLimit
	}
	return publisher
}

func normalizePublishMode(mode PublishMode) PublishMode {
	switch mode {
	case PublishModeShadow, PublishModePGFirst:
		return mode
	default:
		return PublishModeValkeyOnly
	}
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
	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(notifications))
	for i, notification := range notifications {
		if notification == nil {
			return dispatchoutbox.PublishBatchResult{}, fmt.Errorf("publish alarm queue batch: notification %d is nil", i)
		}
		if err := notification.ValidateLegacyRoute(); err != nil {
			return dispatchoutbox.PublishBatchResult{}, fmt.Errorf("publish alarm queue batch: validate legacy route: %w", err)
		}
		var keys []string
		if len(claimKeys) > 0 {
			keys = claimKeys[i]
		}
		envelopes = append(envelopes, p.buildEnvelope(notification, keys))
	}

	result := dispatchoutbox.PublishBatchResult{RequestedDeliveries: len(envelopes)}
	defer func() {
		observeAlarmDispatchPublishBatch(time.Since(startedAt), result)
	}()

	var err error
	switch p.publishConfig.Mode {
	case PublishModeShadow:
		result, err = p.publishShadowBatch(ctx, envelopes)
		return result, err
	case PublishModePGFirst:
		result, err = p.publishPGFirstBatch(ctx, envelopes)
		return result, err
	default:
		result, err = p.publishValkeyBatch(ctx, envelopes)
		return result, err
	}
}

func (p *Publisher) publishShadowBatch(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	result, err := p.publishValkeyBatch(ctx, envelopes)
	if err != nil || p.outbox == nil {
		return result, err
	}
	insertResult, err := p.insertOutboxChunks(ctx, envelopes, dispatchoutbox.StatusShadowed)
	insertResult.RequestedDeliveries = len(envelopes)
	insertResult.ProcessedDeliveries = result.ProcessedDeliveries
	if err != nil {
		p.logger.Warn("Alarm outbox shadow batch insert failed",
			slog.Int("notification_count", len(envelopes)),
			slog.Any("error", err),
		)
		if p.publishConfig.ShadowFatal {
			return insertResult, fmt.Errorf("publish alarm queue batch: insert shadowed outbox: %w", err)
		}
	}
	return insertResult, nil
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

func (p *Publisher) publishValkeyBatch(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	result := dispatchoutbox.PublishBatchResult{RequestedDeliveries: len(envelopes)}
	for i := range envelopes {
		if err := p.publishValkey(ctx, envelopes[i]); err != nil {
			return result, err
		}
		result.ProcessedDeliveries++
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
		end := start + limit
		if end > len(envelopes) {
			end = len(envelopes)
		}
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
		total.ShadowedDuplicates += result.ShadowedDuplicates
		total.PromotedShadowedCount += result.PromotedShadowedCount
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

func (p *Publisher) publishValkey(ctx context.Context, envelope domain.AlarmQueueEnvelope) error {
	jsonBytes, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("publish alarm queue: marshal envelope: %w", err)
	}

	cmd := p.cache.B().Lpush().Key(AlarmDispatchQueue).Element(string(jsonBytes)).Build()
	results := p.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return fmt.Errorf("publish alarm queue: lpush dispatch queue: unexpected result count: %d", len(results))
	}
	if err := results[0].Error(); err != nil {
		return fmt.Errorf("publish alarm queue: lpush dispatch queue: %w", err)
	}

	p.logger.Debug("알림 큐 발행 완료",
		slog.String("room_id", envelope.Notification.RoomID),
		slog.String("queue", AlarmDispatchQueue),
	)
	return nil
}

func (p *Publisher) publishWakeup(ctx context.Context) {
	if p.cache == nil {
		return
	}
	acquired, err := p.cache.SetNX(ctx, alarmDispatchWakeupGuardKey, "1", 3*time.Second)
	if err != nil {
		alarmDispatchWakeupFailedTotal.Inc()
		p.logger.Warn("Alarm outbox wakeup guard failed", slog.Any("error", err))
		return
	}
	if !acquired {
		alarmDispatchWakeupSuppressedTotal.Inc()
		return
	}
	cmd := p.cache.B().Lpush().Key(AlarmDispatchWakeupQueue).Element("1").Build()
	results := p.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		alarmDispatchWakeupFailedTotal.Inc()
		p.logger.Warn("Alarm outbox wakeup failed", slog.Int("result_count", len(results)))
		return
	}
	if err := results[0].Error(); err != nil {
		alarmDispatchWakeupFailedTotal.Inc()
		p.logger.Warn("Alarm outbox wakeup failed", slog.Any("error", err))
		return
	}
	alarmDispatchWakeupSentTotal.Inc()
	if err := p.cache.Expire(ctx, AlarmDispatchWakeupQueue, 2*time.Second); err != nil {
		p.logger.Warn("Alarm outbox wakeup expire failed", slog.Any("error", err))
	}
}
