package youtubedispatch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/claim"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/store"
)

type DeliveryExecutor interface {
	dispatchDeliveryRows(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxByID map[int64]domain.YouTubeNotificationOutbox) dispatchstate.DispatchResult
}

type ClaimResolver interface {
	selectClaimedDeliveries(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxes []domain.YouTubeNotificationOutbox, reuseCache claim.DecisionCache) deliveryClaimSelection
	applyClaimSelection(result *dispatchstate.DispatchResult, mu *sync.Mutex, selection *deliveryClaimSelection)
	releaseDeliveryClaims(ctx context.Context, claims []dispatchstate.ClaimToken) error
	releaseDeliveryClaimsWithWarning(ctx context.Context, claims []dispatchstate.ClaimToken, message string, attrs ...any)
}

type ClaimManager struct {
	db          deliverysql.DeliveryDB
	config      dispatchstate.Config
	logger      *slog.Logger
	delivery    *store.DeliveryRepository
	executor    DeliveryExecutor
	status      *StatusUpdater
	metrics     *MetricsRecorder
	grouper     *OutboxGrouper
	auditLogger *AuditLogger
}

func newClaimManager(
	db deliverysql.DeliveryDB,
	logger *slog.Logger,
	config *dispatchstate.Config,
	deliveryRepo *store.DeliveryRepository,
	executor DeliveryExecutor,
	status *StatusUpdater,
	grouper *OutboxGrouper,
	auditLogger *AuditLogger,
) *ClaimManager {
	if logger == nil {
		logger = slog.Default()
	}

	return &ClaimManager{
		db:          db,
		config:      *config,
		logger:      logger,
		delivery:    deliveryRepo,
		executor:    executor,
		status:      status,
		grouper:     grouper,
		auditLogger: auditLogger,
	}
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

func (d *ClaimManager) markSent(ctx context.Context, id int64, lockedAt *time.Time) {
	d.status.markSentIfLocked(ctx, id, lockedAt)
}

func (d *ClaimManager) markFailed(ctx context.Context, id int64, lockedAt *time.Time, errMsg string) {
	d.status.markFailedIfLocked(ctx, id, lockedAt, errMsg)
}

func (d *ClaimManager) collectRoomsByChannel(ctx context.Context, items []domain.YouTubeNotificationOutbox) map[string]channelAlarmRoomTargets {
	return d.grouper.collectRoomsByChannel(ctx, items)
}

func (d *ClaimManager) filterLiveCatchupSuppressedRooms(
	ctx context.Context,
	item *domain.YouTubeNotificationOutbox,
	rooms map[string]bool,
) map[string]bool {
	return d.grouper.filterLiveCatchupSuppressedRooms(ctx, item, rooms)
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

func (d *ClaimManager) recordDeliveryFailure(
	result *dispatchstate.DispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
) {
	if d != nil && d.metrics != nil {
		d.metrics.recordDeliveryFailure(result, mu, reason, deliveryID, outboxID)

		return
	}

	mu.Lock()

	result.FailedDeliveries++

	result.FailureBuckets[reason] = append(result.FailureBuckets[reason], deliveryID)
	result.TouchedOutboxIDs = append(result.TouchedOutboxIDs, outboxID)
	mu.Unlock()
}
