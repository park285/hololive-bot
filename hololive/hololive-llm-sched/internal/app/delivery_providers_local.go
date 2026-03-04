package app

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

// ProvideDeliveryLocker - 분산 락 인스턴스 생성
func ProvideDeliveryLocker(cacheSvc cache.Client, logger *slog.Logger) delivery.NotificationLocker {
	return delivery.NewLocker(cacheSvc, logger)
}

// ProvideOutboxRepository - 알림 delivery outbox 저장소 생성
func ProvideOutboxRepository(postgres database.Client, logger *slog.Logger) *delivery.OutboxRepository {
	return delivery.NewOutboxRepository(postgres.GetGormDB(), logger)
}

// ProvideDeliveryDispatcher - outbox 발송 워커 생성
func ProvideDeliveryDispatcher(repo *delivery.OutboxRepository, sender delivery.MessageSender, logger *slog.Logger) *delivery.Dispatcher {
	return delivery.NewDispatcher(repo, sender, logger, delivery.DefaultDispatcherConfig())
}
