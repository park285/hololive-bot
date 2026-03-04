package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// AlarmDispatchQueue: Valkey 큐 키
const AlarmDispatchQueue = contractsalarm.DispatchQueueKey

// Publisher: 알림 봉투를 Valkey List로 발행하는 퍼블리셔
type Publisher struct {
	cache  cache.Client
	logger *slog.Logger
}

// NewPublisher: QueuePublisher 생성
func NewPublisher(c cache.Client, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}

	return &Publisher{
		cache:  c,
		logger: logger,
	}
}

// Publish: 알림 봉투를 JSON 직렬화 후 Valkey 큐에 LPUSH 한다.
func (p *Publisher) Publish(ctx context.Context, notification *domain.AlarmNotification, claimKeys []string) error {
	if notification == nil {
		return fmt.Errorf("publish alarm queue: notification is nil")
	}

	envelope := domain.AlarmQueueEnvelope{
		Notification: *notification,
		ClaimKeys:    claimKeys,
		EnqueuedAt:   time.Now().UTC().Format(time.RFC3339),
		Version:      contractsalarm.QueueEnvelopeVersionV1,
	}

	jsonBytes, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("publish alarm queue: marshal envelope: %w", err)
	}

	cmd := p.cache.B().Lpush().Key(AlarmDispatchQueue).Element(string(jsonBytes)).Build()
	if err := p.cache.GetClient().Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("publish alarm queue: lpush dispatch queue: %w", err)
	}

	p.logger.Debug("알림 큐 발행 완료",
		slog.String("room_id", notification.RoomID),
		slog.String("queue", AlarmDispatchQueue),
	)
	return nil
}
