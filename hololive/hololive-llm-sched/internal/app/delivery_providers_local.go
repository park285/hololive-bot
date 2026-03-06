package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/iris"
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
