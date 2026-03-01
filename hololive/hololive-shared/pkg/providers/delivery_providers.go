package providers

import (
	"fmt"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/notification"
)

// ProvideDeliveryLocker - 분산 락 인스턴스 생성
func ProvideDeliveryLocker(cacheSvc *cache.Service, logger *slog.Logger) delivery.NotificationLocker {
	return delivery.NewLocker(cacheSvc, logger)
}

// ProvideOutboxRepository - 알림 delivery outbox 저장소 생성
func ProvideOutboxRepository(postgres *database.PostgresService, logger *slog.Logger) *delivery.OutboxRepository {
	return delivery.NewOutboxRepository(postgres.GetGormDB(), logger)
}

// ProvideDeliveryDispatcher - outbox 발송 워커 생성
func ProvideDeliveryDispatcher(repo *delivery.OutboxRepository, sender delivery.MessageSender, logger *slog.Logger) *delivery.Dispatcher {
	return delivery.NewDispatcher(repo, sender, logger, delivery.DefaultDispatcherConfig())
}

// ProvideAlarmQueueDispatcher: 알림 큐 디스패처 생성 (비활성 시 nil)
func ProvideAlarmQueueDispatcher(
	enabled bool,
	cacheSvc *cache.Service,
	alarmSvc domain.AlarmDispatchState,
	irisClient iris.Client,
	formatter *adapter.ResponseFormatter,
	logger *slog.Logger,
) *notification.AlarmQueueDispatcher {
	if !enabled {
		logger.Info("Alarm queue consumer disabled by config")
		return nil
	}
	return notification.NewAlarmQueueDispatcher(
		cacheSvc.GetClient(),
		alarmSvc,
		irisClient,
		formatter,
		logger,
	)
}

// ProvideAlarmWorkerPool - 알림 처리용 워커풀 생성
func ProvideAlarmWorkerPool() (*workerpool.Pool, error) {
	cfg := workerpool.DefaultConfig()
	cfg.Size = 10
	pool, err := workerpool.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create alarm worker pool: %w", err)
	}
	return pool, nil
}
