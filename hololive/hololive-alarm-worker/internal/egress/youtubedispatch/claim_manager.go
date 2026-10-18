package youtubedispatch

import (
	"context"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

type DeliveryExecutor interface {
	dispatchDeliveryRows(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxByID map[int64]domain.YouTubeNotificationOutbox) dispatchstate.DispatchResult
}

type ClaimResolver interface {
	selectClaimedDeliveries(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxes []domain.YouTubeNotificationOutbox, reuseCache *claimDecisionCache) deliveryClaimSelection
	releaseDeliveryClaimsWithWarning(ctx context.Context, claims []dispatchstate.ClaimToken, message string, attrs ...any)
}

type ClaimManager struct {
	db           deliverysql.DeliveryDB
	config       dispatchstate.Config
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
	config *dispatchstate.Config,
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

// setExecutor는 SendEngine과 ClaimManager의 상호 참조 때문에 assembleDispatcher가 생성 직후 한 번 호출한다.
func (d *ClaimManager) setExecutor(executor DeliveryExecutor) {
	d.executor = executor
}

func (d *ClaimManager) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) dispatchstate.DispatchResult {
	return d.executor.dispatchDeliveryRows(ctx, rows, outboxByID)
}
