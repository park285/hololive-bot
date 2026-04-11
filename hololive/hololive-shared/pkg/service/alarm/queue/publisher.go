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

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

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
	if err := notification.ValidateLegacyRoute(); err != nil {
		return fmt.Errorf("publish alarm queue: validate legacy route: %w", err)
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
	results := p.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return fmt.Errorf("publish alarm queue: lpush dispatch queue: unexpected result count: %d", len(results))
	}
	if err := results[0].Error(); err != nil {
		return fmt.Errorf("publish alarm queue: lpush dispatch queue: %w", err)
	}

	p.logger.Debug("알림 큐 발행 완료",
		slog.String("room_id", notification.RoomID),
		slog.String("queue", AlarmDispatchQueue),
	)
	return nil
}
