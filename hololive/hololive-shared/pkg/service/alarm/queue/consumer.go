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
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const ClaimKeyPrefix = contractsalarm.NotifyClaimKeyPrefix

const retryMemberPrefix = "retry-member:v1:"

type Consumer struct {
	cache         cache.Client
	queueKey      string
	retryQueueKey string
	dlqKey        string
	blockTimeout  time.Duration
	drainTimeout  time.Duration
	maxBatch      int
	retryBatchSeq uint64
	logger        *slog.Logger
}

type ConsumerOption func(*Consumer)

func WithQueueKey(key string) ConsumerOption {
	return func(c *Consumer) {
		c.queueKey = key
		c.retryQueueKey = deriveRetryQueueKey(key)
		c.dlqKey = deriveDLQKey(key)
	}
}

func WithMaxBatch(n int) ConsumerOption {
	return func(c *Consumer) { c.maxBatch = n }
}

func NewConsumer(c cache.Client, logger *slog.Logger, opts ...ConsumerOption) *Consumer {
	if logger == nil {
		logger = slog.Default()
	}
	initQueueMetrics()

	consumer := &Consumer{
		cache:         c,
		queueKey:      contractsalarm.DispatchQueueKey,
		retryQueueKey: contractsalarm.DispatchRetryQueueKey,
		dlqKey:        contractsalarm.DispatchDLQKey,
		blockTimeout:  1 * time.Second,
		drainTimeout:  50 * time.Millisecond,
		maxBatch:      50,
		logger:        logger,
	}
	for _, opt := range opts {
		opt(consumer)
	}
	consumer.retryQueueKey = deriveRetryQueueKey(consumer.queueKey)
	consumer.dlqKey = deriveDLQKey(consumer.queueKey)
	return consumer
}

// 첫 항목은 blockTimeout으로 대기, 이후 항목은 drainTimeout으로 짧게 반복 조회한다.
func (c *Consumer) DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
	initQueueMetrics()

	limit := max(1, min(maxItems, c.maxBatch))

	startedAt := time.Now()
	resultLabel := "ok"
	envelopes := make([]domain.AlarmQueueEnvelope, 0, limit)
	defer func() {
		alarmQueueDrainDuration.Observe(time.Since(startedAt).Seconds())
		alarmQueueDrainBatch.Observe(float64(len(envelopes)))
		alarmQueueDrainTotal.WithLabelValues(resultLabel).Inc()
	}()

	delayed, err := c.drainDelayedRetries(ctx, limit, time.Now().UTC())
	if err != nil {
		resultLabel = "error"
		return nil, fmt.Errorf("drain queue batch: pop delayed retry payloads: %w", err)
	}
	envelopes = c.appendAcceptedPayloads(ctx, "drain_delayed", delayed, envelopes)

	remaining := limit - len(envelopes)
	if remaining <= 0 {
		return envelopes, nil
	}

	if len(delayed) > 0 {
		drained, err := c.rpopMany(ctx, remaining)
		if err != nil {
			resultLabel = "error"
			return nil, fmt.Errorf("drain queue batch: pop active payloads after delayed retries: %w", err)
		}
		envelopes = c.appendAcceptedPayloads(ctx, "drain", drained, envelopes)
		return envelopes, nil
	}

	envelopes, resultLabel, err = c.drainActiveAfterBlock(ctx, envelopes, limit)
	if err != nil {
		return nil, err
	}
	return envelopes, nil
}

func (c *Consumer) drainActiveAfterBlock(
	ctx context.Context,
	envelopes []domain.AlarmQueueEnvelope,
	limit int,
) ([]domain.AlarmQueueEnvelope, string, error) {
	firstRaw, err := c.brpop(ctx, c.blockTimeout)
	if err != nil {
		return envelopes, "error", fmt.Errorf("drain queue batch: pop first payload: %w", err)
	}
	if firstRaw == "" {
		return envelopes, "empty", nil
	}

	envelopes = c.appendAcceptedPayloads(ctx, "drain", []string{firstRaw}, envelopes)

	remaining := limit - len(envelopes)
	if remaining <= 0 {
		return envelopes, "ok", nil
	}

	drained, err := c.rpopMany(ctx, remaining)
	if err != nil {
		return envelopes, "error", fmt.Errorf("drain queue batch: pop drain payloads: %w", err)
	}
	envelopes = c.appendAcceptedPayloads(ctx, "drain", drained, envelopes)

	return envelopes, "ok", nil
}
