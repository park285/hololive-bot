package youtubedispatch

import (
	"context"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type DeliveryExecutor interface {
	dispatchDeliveryRows(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxByID map[int64]domain.YouTubeNotificationOutbox) DispatchResult
}

type ClaimManager struct {
	db           deliverysql.DeliveryDB
	config       Config
	logger       *slog.Logger
	delivery     *store.DeliveryRepository
	transition   *store.TransitionStore
	fanout       *OutboxFanoutService
	projector    *OutboxAggregateProjector
	executor     DeliveryExecutor
	grouper      *OutboxGrouper
	auditLogger  *AuditLogger
	workerTotals *workercontract.Counters
}

func newClaimManager(
	db deliverysql.DeliveryDB,
	logger *slog.Logger,
	config *Config,
	deliveryRepo *store.DeliveryRepository,
	transitionStore *store.TransitionStore,
	executor DeliveryExecutor,
	grouper *OutboxGrouper,
	auditLogger *AuditLogger,
) *ClaimManager {
	if logger == nil {
		logger = slog.Default()
	}

	manager := &ClaimManager{
		db:          db,
		config:      *config,
		logger:      logger,
		delivery:    deliveryRepo,
		transition:  transitionStore,
		executor:    executor,
		grouper:     grouper,
		auditLogger: auditLogger,
	}

	manager.fanout = newOutboxFanoutService(transitionStore, grouper, logger, config)
	manager.projector = newOutboxAggregateProjector(deliveryRepo)

	return manager
}

func (d *ClaimManager) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) DispatchResult {
	return d.executor.dispatchDeliveryRows(ctx, rows, outboxByID)
}
