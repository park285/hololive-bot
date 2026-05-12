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
	"strings"
	"sync/atomic"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *Consumer) ScheduleRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if len(envelopes) == 0 {
		return nil
	}

	cmds := make([]valkey.Completed, 0, len(envelopes))
	now := time.Now().UTC()
	batchToken := atomic.AddUint64(&c.retryBatchSeq, 1)
	for i := range envelopes {
		normalized, accepted := c.acceptLegacyEnvelope(ctx, envelopes[i], "schedule_retry")
		if !accepted {
			continue
		}
		normalized.EnsureSourcePayloadFromRaw()

		nextVisibleAt, err := resolveRetryVisibleAt(normalized, now)
		if err != nil {
			return fmt.Errorf("schedule retry envelopes: resolve next visible at: %w", err)
		}

		jsonBytes, err := json.Marshal(normalized)
		if err != nil {
			return fmt.Errorf("schedule retry envelopes: marshal envelope: %w", err)
		}

		member := buildRetryMember(nextVisibleAt.UnixMilli(), batchToken, len(cmds), string(jsonBytes))
		cmds = append(cmds, c.cache.B().Zadd().
			Key(c.retryQueueKey).
			ScoreMember().
			ScoreMember(float64(nextVisibleAt.UnixMilli()), member).
			Build())
	}

	if len(cmds) == 0 {
		return nil
	}

	results := c.cache.DoMulti(ctx, cmds...)
	for _, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("schedule retry envelopes: zadd retry queue: %w", err)
		}
	}
	alarmQueueRetryScheduled.Add(float64(len(results)))
	return nil
}

func (c *Consumer) MoveToDLQ(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if len(envelopes) == 0 {
		return nil
	}

	elements := make([]string, 0, len(envelopes))
	for i := range envelopes {
		jsonBytes, err := json.Marshal(envelopes[i])
		if err != nil {
			return fmt.Errorf("move envelopes to dlq: marshal envelope: %w", err)
		}

		if shouldPreferOriginalPayload(envelopes[i], string(jsonBytes)) {
			elements = append(elements, envelopes[i].OriginalPayload())
			continue
		}
		elements = append(elements, string(jsonBytes))
	}

	cmd := c.cache.B().Lpush().Key(c.dlqKey).Element(elements...).Build()
	results := c.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return fmt.Errorf("move envelopes to dlq: lpush dlq: unexpected result count: %d", len(results))
	}
	if err := results[0].Error(); err != nil {
		return fmt.Errorf("move envelopes to dlq: lpush dlq: %w", err)
	}

	alarmQueueDLQMoved.Add(float64(len(elements)))
	return nil
}

func (c *Consumer) Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error {
	if len(envelopes) == 0 {
		return nil
	}

	elements := make([]string, 0, len(envelopes))
	for i := range envelopes {
		normalized, accepted := c.acceptLegacyEnvelope(ctx, envelopes[i], "requeue")
		if !accepted {
			continue
		}

		jsonBytes, err := json.Marshal(normalized)
		if err != nil {
			return fmt.Errorf("requeue envelopes: marshal envelope: %w", err)
		}
		elements = append(elements, string(jsonBytes))
	}

	if len(elements) == 0 {
		return nil
	}

	cmd := c.cache.B().Lpush().Key(c.queueKey).Element(elements...).Build()
	results := c.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return fmt.Errorf("requeue envelopes: lpush dispatch queue: unexpected result count: %d", len(results))
	}
	if err := results[0].Error(); err != nil {
		return fmt.Errorf("requeue envelopes: lpush dispatch queue: %w", err)
	}

	return nil
}

func (c *Consumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	initQueueMetrics()

	filtered := make([]string, 0, len(claimKeys))
	for _, key := range claimKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" && strings.HasPrefix(trimmed, ClaimKeyPrefix) {
			filtered = append(filtered, trimmed)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	if _, err := c.cache.DelMany(ctx, filtered); err != nil {
		return fmt.Errorf("release claim keys: del filtered keys: %w", err)
	}
	alarmQueueClaimReleased.Add(float64(len(filtered)))
	return nil
}
