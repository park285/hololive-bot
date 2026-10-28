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
	AlarmDispatchWakeupQueue    = "alarm:dispatch:wakeup"
	alarmDispatchWakeupGuardKey = "alarm:dispatch:wakeup:guard"
)

type PublishConfig struct {
	WakeupEnabled         bool
	MaxDeliveriesPerBatch int
}

// WakeupCache는 Publisher가 dispatch wakeup 신호에 쓰는 cache 작업이다. 중복 신호 guard 선점(SetNX),
// 신호 LPUSH(B·DoMulti), 신호 queue 만료(Expire)를 쓴다.
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

// NewPublisher는 PG dispatch outbox에 pending 행을 쓰고 wakeup을 보내는 발행자를 만든다. 유일한 발행 경로인
// outbox는 필수이고, batch 한도는 RuntimeConfig.AlarmDispatchPublish가 검증한 양수여야 한다. 기본값으로 바꾸지
// 않고 생성 오류로 드러낸다.
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

	initQueueMetrics()

	return &Publisher{
		cache:         c,
		outbox:        outbox,
		publishConfig: config,
		logger:        logger,
		now:           time.Now,
	}, nil
}

func (p *Publisher) Publish(ctx context.Context, notification *domain.AlarmNotification, claimKeys []string) (dispatchoutbox.PublishBatchResult, error) {
	out, err := p.PublishBatch(ctx, []*domain.AlarmNotification{notification}, [][]string{claimKeys})
	if err != nil {
		return out, fmt.Errorf("publish batch: %w", err)
	}

	return out, nil
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

// PublishDispatchBatch는 envelope을 pending delivery로 저장한다.
func (p *Publisher) PublishDispatchBatch(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
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

func (p *Publisher) prepareDispatchEnvelopes(envelopes []domain.AlarmQueueEnvelope) error {
	for i := range envelopes {
		if err := envelopes[i].ValidateCanonicalDispatch(); err != nil {
			return fmt.Errorf("publish alarm dispatch batch: validate envelope %d: %w", i, err)
		}

		if strings.TrimSpace(envelopes[i].EnqueuedAt) == "" {
			envelopes[i].EnqueuedAt = p.now().UTC().Format(time.RFC3339)
		}

		// 버전이 없는 envelope을 V1으로 채우던 기본값은 지웠다. 모든 생산자가 버전을 명시하므로 다른 값은 생산자 결함이다.
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
) ([]domain.AlarmQueueEnvelope, error) {
	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(notifications))
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

func (p *Publisher) insertOutboxChunks(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	var total dispatchoutbox.PublishBatchResult

	limit := p.publishConfig.MaxDeliveriesPerBatch

	for start := 0; start < len(envelopes); start += limit {
		end := min(start+limit, len(envelopes))

		result, err := p.outbox.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: envelopes[start:end]})
		// 뒤 chunk가 실패해도 이미 commit 또는 ledger 조회로 확인한 receipt는 보존한다.
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
