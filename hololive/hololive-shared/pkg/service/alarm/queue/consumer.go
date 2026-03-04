package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/util"
)

// ClaimKeyPrefix: claim 키 접두사 (release 시 필터링용)
const ClaimKeyPrefix = contractsalarm.NotifyClaimKeyPrefix

// Consumer: Valkey BRPOP 기반 큐 소비자
type Consumer struct {
	cache        cache.Client
	queueKey     string
	blockTimeout time.Duration
	drainTimeout time.Duration
	maxBatch     int
	logger       *slog.Logger
}

// ConsumerOption: Consumer 설정 옵션
type ConsumerOption func(*Consumer)

// WithQueueKey: 큐 키 변경
func WithQueueKey(key string) ConsumerOption {
	return func(c *Consumer) { c.queueKey = key }
}

// WithMaxBatch: 최대 배치 크기 변경
func WithMaxBatch(n int) ConsumerOption {
	return func(c *Consumer) { c.maxBatch = n }
}

// NewConsumer: QueueConsumer 생성
func NewConsumer(c cache.Client, logger *slog.Logger, opts ...ConsumerOption) *Consumer {
	if logger == nil {
		logger = slog.Default()
	}

	consumer := &Consumer{
		cache:        c,
		queueKey:     contractsalarm.DispatchQueueKey,
		blockTimeout: 1 * time.Second,
		drainTimeout: 50 * time.Millisecond,
		maxBatch:     50,
		logger:       logger,
	}
	for _, opt := range opts {
		opt(consumer)
	}
	return consumer
}

// DrainBatch: BRPOP으로 큐에서 최대 maxItems개의 envelope을 꺼낸다.
// 첫 항목은 blockTimeout으로 대기, 이후 항목은 drainTimeout으로 짧게 반복 조회한다.
func (c *Consumer) DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error) {
	limit := min(maxItems, c.maxBatch)
	if limit < 1 {
		limit = 1
	}

	envelopes := make([]domain.AlarmQueueEnvelope, 0, limit)

	firstRaw, err := c.brpop(ctx, c.blockTimeout)
	if err != nil {
		return nil, fmt.Errorf("drain queue batch: pop first payload: %w", err)
	}
	if firstRaw == "" {
		return envelopes, nil
	}

	if envelope, ok := parseEnvelope(firstRaw, c.logger); ok {
		envelopes = append(envelopes, envelope)
	}

	for len(envelopes) < limit {
		raw, err := c.brpop(ctx, c.drainTimeout)
		if err != nil {
			return nil, fmt.Errorf("drain queue batch: pop drain payload: %w", err)
		}
		if raw == "" {
			break
		}

		if envelope, ok := parseEnvelope(raw, c.logger); ok {
			envelopes = append(envelopes, envelope)
		}
	}

	return envelopes, nil
}

// ReleaseClaimKeys: claim 키를 prefix 필터링 후 Valkey DEL
func (c *Consumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
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
	return nil
}

// brpop: Valkey BRPOP 래퍼
func (c *Consumer) brpop(ctx context.Context, timeout time.Duration) (string, error) {
	cmd := c.cache.B().Brpop().Key(c.queueKey).Timeout(timeout.Seconds()).Build()
	result, err := c.cache.GetClient().Do(ctx, cmd).AsStrSlice()
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

// parseEnvelope: JSON을 AlarmQueueEnvelope로 파싱 (v0/v1 지원)
func parseEnvelope(raw string, logger *slog.Logger) (domain.AlarmQueueEnvelope, bool) {
	var envelope domain.AlarmQueueEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		logger.Warn("failed to parse alarm queue envelope", slog.String("error", err.Error()))
		return domain.AlarmQueueEnvelope{}, false
	}

	switch envelope.Version {
	case 0, contractsalarm.QueueEnvelopeVersionV1:
		return envelope, true
	default:
		logger.Warn("unsupported alarm queue envelope version", slog.Uint64("version", uint64(envelope.Version)))
		return domain.AlarmQueueEnvelope{}, false
	}
}
