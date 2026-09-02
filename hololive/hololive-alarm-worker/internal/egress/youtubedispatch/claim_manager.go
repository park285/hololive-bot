package youtubedispatch

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/claim"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

type DeliveryExecutor interface {
	dispatchDeliveryRows(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxByID map[int64]domain.YouTubeNotificationOutbox) dispatchstate.DispatchResult
}

type ClaimResolver interface {
	selectClaimedDeliveries(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxes []domain.YouTubeNotificationOutbox, reuseCache claim.DecisionCache) deliveryClaimSelection
	releaseDeliveryClaims(ctx context.Context, claims []dispatchstate.ClaimToken) error
	releaseDeliveryClaimsWithWarning(ctx context.Context, claims []dispatchstate.ClaimToken, message string, attrs ...any)
}

type ClaimManager struct {
	db          deliverysql.DeliveryDB
	config      dispatchstate.Config
	logger      *slog.Logger
	delivery    *store.DeliveryRepository
	transition  *store.TransitionStore
	fanout      *OutboxFanoutService
	projector   *OutboxAggregateProjector
	executor    DeliveryExecutor
	metrics     *MetricsRecorder
	grouper     *OutboxGrouper
	auditLogger *AuditLogger
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

func (d *ClaimManager) setExecutor(executor DeliveryExecutor) {
	if d != nil {
		d.executor = executor
	}
}

func (d *ClaimManager) setMetricsRecorder(metrics *MetricsRecorder) {
	if d != nil {
		d.metrics = metrics
	}
}

func (d *ClaimManager) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) dispatchstate.DispatchResult {
	if d == nil || d.executor == nil {
		return dispatchstate.DispatchResult{
			SuccessDeliveryIDs: make([]int64, 0, len(rows)),
			TouchedOutboxIDs:   make([]int64, 0, len(rows)),
			SuccessClaimTokens: make([]dispatchstate.ClaimToken, 0, len(rows)),
			FailureBuckets:     make(map[string][]int64),
		}
	}

	return d.executor.dispatchDeliveryRows(ctx, rows, outboxByID)
}
