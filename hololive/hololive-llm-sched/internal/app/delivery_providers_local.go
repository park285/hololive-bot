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

package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type irisDeliverySender struct {
	client iris.Client
}

func (s irisDeliverySender) SendMessage(ctx context.Context, roomID, message string) error {
	if err := s.client.SendMessage(ctx, roomID, message); err != nil {
		return fmt.Errorf("iris send message: %w", err)
	}
	return nil
}

// ProvideDeliveryLocker - 분산 락 인스턴스 생성
func ProvideDeliveryLocker(cacheSvc cache.Client, logger *slog.Logger) delivery.NotificationLocker {
	return delivery.NewLocker(cacheSvc, logger)
}

// ProvideOutboxRepository - 알림 delivery outbox 저장소 생성
func ProvideOutboxRepository(postgres database.Client, logger *slog.Logger) *delivery.OutboxRepository {
	return delivery.NewOutboxRepository(postgres.GetGormDB(), logger)
}

// ProvideDeliverySender - Iris client를 delivery.MessageSender로 어댑트한다.
func ProvideDeliverySender(client iris.Client) delivery.MessageSender {
	return irisDeliverySender{client: client}
}

// ProvideDeliveryDispatcher - outbox 발송 워커 생성
func ProvideDeliveryDispatcher(repo *delivery.OutboxRepository, sender delivery.MessageSender, logger *slog.Logger) *delivery.Dispatcher {
	return delivery.NewDispatcher(repo, sender, logger, delivery.DefaultDispatcherConfig())
}
