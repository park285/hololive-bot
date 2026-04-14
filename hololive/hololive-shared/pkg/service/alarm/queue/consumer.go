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
	"encoding/base64"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/util"
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
	for _, raw := range delayed {
		if envelope, ok := parseEnvelope(raw, c.logger); ok {
			if normalized, accepted := c.acceptLegacyEnvelope(ctx, envelope, "drain_delayed"); accepted {
				envelopes = append(envelopes, normalized)
			}
		}
	}

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
		for _, raw := range drained {
			if envelope, ok := parseEnvelope(raw, c.logger); ok {
				if normalized, accepted := c.acceptLegacyEnvelope(ctx, envelope, "drain"); accepted {
					envelopes = append(envelopes, normalized)
				}
			}
		}
		return envelopes, nil
	}

	firstRaw, err := c.brpop(ctx, c.blockTimeout)
	if err != nil {
		resultLabel = "error"
		return nil, fmt.Errorf("drain queue batch: pop first payload: %w", err)
	}
	if firstRaw == "" {
		resultLabel = "empty"
		return envelopes, nil
	}

	if envelope, ok := parseEnvelope(firstRaw, c.logger); ok {
		if normalized, accepted := c.acceptLegacyEnvelope(ctx, envelope, "drain"); accepted {
			envelopes = append(envelopes, normalized)
		}
	}

	remaining = limit - len(envelopes)
	if remaining <= 0 {
		return envelopes, nil
	}

	drained, err := c.rpopMany(ctx, remaining)
	if err != nil {
		resultLabel = "error"
		return nil, fmt.Errorf("drain queue batch: pop drain payloads: %w", err)
	}
	for _, raw := range drained {
		if envelope, ok := parseEnvelope(raw, c.logger); ok {
			if normalized, accepted := c.acceptLegacyEnvelope(ctx, envelope, "drain"); accepted {
				envelopes = append(envelopes, normalized)
			}
		}
	}

	return envelopes, nil
}

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

func (c *Consumer) acceptLegacyEnvelope(
	ctx context.Context,
	envelope domain.AlarmQueueEnvelope,
	source string,
) (domain.AlarmQueueEnvelope, bool) {
	if envelope.Notification.AlarmType == "" {
		envelope.Notification.AlarmType = domain.AlarmTypeLive
	}
	if err := envelope.Notification.ValidateLegacyRoute(); err != nil {
		alarmQueueEnvelopeTotal.WithLabelValues("rejected_legacy_route").Inc()
		c.logger.Warn("dropping unsupported legacy alarm queue envelope",
			slog.String("source", source),
			slog.String("queue", c.queueKey),
			slog.String("room_id", strings.TrimSpace(envelope.Notification.RoomID)),
			slog.String("alarm_type", string(envelope.Notification.AlarmType)),
			slog.Any("error", err),
		)
		if releaseErr := c.ReleaseClaimKeys(ctx, envelope.ClaimKeys); releaseErr != nil {
			c.logger.Warn("failed to release claim keys for dropped alarm queue envelope",
				slog.String("source", source),
				slog.String("queue", c.queueKey),
				slog.String("room_id", strings.TrimSpace(envelope.Notification.RoomID)),
				slog.Any("error", releaseErr),
			)
		}
		return domain.AlarmQueueEnvelope{}, false
	}

	return envelope, true
}

const drainDelayedRetriesScript = `
local members = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
if #members == 0 then
  return members
end
redis.call('ZREM', KEYS[1], unpack(members))
return members`

func (c *Consumer) drainDelayedRetries(ctx context.Context, count int, now time.Time) ([]string, error) {
	if count <= 0 {
		return nil, nil
	}

	cmd := c.cache.B().Eval().
		Script(drainDelayedRetriesScript).
		Numkeys(1).
		Key(c.retryQueueKey).
		Arg(strconv.FormatInt(now.UnixMilli(), 10), strconv.Itoa(count)).
		Build()
	results := c.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return nil, fmt.Errorf("drain delayed retry payloads: unexpected result count: %d", len(results))
	}
	values, err := results[0].AsStrSlice()
	if err != nil {
		if util.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("drain delayed retry payloads: execute script: %w", err)
	}
	if len(values) > 0 {
		alarmQueueRetryDrained.Add(float64(len(values)))
	}
	payloads := make([]string, 0, len(values))
	for _, value := range values {
		payload, err := unwrapRetryMember(value)
		if err != nil {
			return nil, fmt.Errorf("drain delayed retry payloads: unwrap member: %w", err)
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

// brpop: Valkey BRPOP 래퍼
func (c *Consumer) brpop(ctx context.Context, timeout time.Duration) (string, error) {
	cmd := c.cache.B().Brpop().Key(c.queueKey).Timeout(timeout.Seconds()).Build()
	results := c.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return "", fmt.Errorf("brpop queue payload: unexpected result count: %d", len(results))
	}
	result, err := results[0].AsStrSlice()
	if err != nil {
		if util.IsValkeyNil(err) {
			return "", nil
		}
		return "", fmt.Errorf("brpop queue payload: execute command: %w", err)
	}

	// BRPOP은 [key, value] 쌍 반환
	if len(result) < 2 {
		return "", nil
	}
	return result[1], nil
}

func (c *Consumer) rpopMany(ctx context.Context, count int) ([]string, error) {
	if count <= 0 {
		return nil, nil
	}

	cmd := c.cache.B().Rpop().Key(c.queueKey).Count(int64(count)).Build()
	results := c.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return nil, fmt.Errorf("rpop queue payloads: unexpected result count: %d", len(results))
	}
	values, err := results[0].AsStrSlice()
	if err != nil {
		if util.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("rpop queue payloads: execute command: %w", err)
	}
	return values, nil
}

func resolveRetryVisibleAt(envelope domain.AlarmQueueEnvelope, now time.Time) (time.Time, error) {
	if envelope.Retry == nil {
		return time.Time{}, fmt.Errorf("retry metadata is required")
	}
	if trimmed := strings.TrimSpace(envelope.Retry.NextVisibleAt); trimmed != "" {
		nextVisibleAt, err := time.Parse(time.RFC3339Nano, trimmed)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse retry next_visible_at: %w", err)
		}
		return nextVisibleAt.UTC(), nil
	}
	if envelope.Retry.RetryAfterMS < 0 {
		return time.Time{}, fmt.Errorf("retry_after_ms must be zero or greater")
	}
	return now.Add(time.Duration(envelope.Retry.RetryAfterMS) * time.Millisecond), nil
}

func deriveRetryQueueKey(queueKey string) string {
	base := strings.TrimSpace(queueKey)
	if base == "" {
		base = contractsalarm.DispatchQueueKey
	}
	if strings.HasSuffix(base, ":queue") {
		return strings.TrimSuffix(base, ":queue") + ":retry"
	}
	return base + ":retry"
}

func deriveDLQKey(queueKey string) string {
	base := strings.TrimSpace(queueKey)
	if base == "" {
		base = contractsalarm.DispatchQueueKey
	}
	if strings.HasSuffix(base, ":queue") {
		return strings.TrimSuffix(base, ":queue") + ":dlq"
	}
	return base + ":dlq"
}

func shouldPreferOriginalPayload(envelope domain.AlarmQueueEnvelope, currentPayload string) bool {
	originalPayload := envelope.OriginalPayload()
	if originalPayload == "" {
		return false
	}
	return currentPayload == envelope.NormalizedPayload()
}

func buildRetryMember(nextVisibleAtMillis int64, batchToken uint64, index int, payload string) string {
	return fmt.Sprintf(
		"%s%013d:%020d:%06d:%s",
		retryMemberPrefix,
		nextVisibleAtMillis,
		batchToken,
		index,
		base64.RawStdEncoding.EncodeToString([]byte(payload)),
	)
}

func unwrapRetryMember(member string) (string, error) {
	if !strings.HasPrefix(member, retryMemberPrefix) {
		return member, nil
	}

	parts := strings.SplitN(strings.TrimPrefix(member, retryMemberPrefix), ":", 4)
	if len(parts) != 4 {
		return "", fmt.Errorf("invalid retry member wrapper")
	}

	payload, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return "", fmt.Errorf("decode retry member payload: %w", err)
	}
	return string(payload), nil
}

// parseEnvelope: JSON을 AlarmQueueEnvelope로 파싱 (v0/v1 지원)
func parseEnvelope(raw string, logger *slog.Logger) (domain.AlarmQueueEnvelope, bool) {
	initQueueMetrics()

	var envelope domain.AlarmQueueEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		alarmQueueEnvelopeTotal.WithLabelValues("invalid").Inc()
		logger.Warn("failed to parse alarm queue envelope", slog.String("error", err.Error()))
		return domain.AlarmQueueEnvelope{}, false
	}

	switch envelope.Version {
	case 0, contractsalarm.QueueEnvelopeVersionV1:
		alarmQueueEnvelopeTotal.WithLabelValues("accepted").Inc()
		return envelope, true
	default:
		alarmQueueEnvelopeTotal.WithLabelValues("unsupported").Inc()
		logger.Warn("unsupported alarm queue envelope version", slog.Uint64("version", uint64(envelope.Version)))
		return domain.AlarmQueueEnvelope{}, false
	}
}
