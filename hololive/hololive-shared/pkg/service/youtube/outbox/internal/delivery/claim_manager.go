package delivery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache/claim"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

type DeliveryExecutor interface {
	dispatchDeliveryRows(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxByID map[int64]domain.YouTubeNotificationOutbox) deliveryDispatchResult
}

type ClaimResolver interface {
	selectClaimedDeliveries(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxes []domain.YouTubeNotificationOutbox, reuseCache claim.DecisionCache) deliveryClaimSelection
	applyClaimSelection(result *deliveryDispatchResult, mu *sync.Mutex, selection deliveryClaimSelection)
	releaseDeliveryClaims(ctx context.Context, claims []deliveryClaimToken) error
	releaseDeliveryClaimsWithWarning(ctx context.Context, claims []deliveryClaimToken, message string, attrs ...any)
}

type ClaimManager struct {
	db          deliverysql.DeliveryDB
	config      Config
	logger      *slog.Logger
	delivery    *DeliveryRepository
	executor    DeliveryExecutor
	status      *StatusUpdater
	metrics     *MetricsRecorder
	grouper     *OutboxGrouper
	auditLogger *AuditLogger
}

func newClaimManager(
	db deliverysql.DeliveryDB,
	logger *slog.Logger,
	config Config,
	deliveryRepo *DeliveryRepository,
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
		config:      config,
		logger:      logger,
		delivery:    deliveryRepo,
		executor:    executor,
		status:      status,
		grouper:     grouper,
		auditLogger: auditLogger,
	}
}

func (c *ClaimManager) setExecutor(executor DeliveryExecutor) {
	if c != nil {
		c.executor = executor
	}
}

func (c *ClaimManager) setMetricsRecorder(metrics *MetricsRecorder) {
	if c != nil {
		c.metrics = metrics
	}
}

func (c *ClaimManager) statusUpdater() *StatusUpdater {
	if c == nil {
		return newStatusUpdater(nil, nil, Config{})
	}
	if c.status != nil {
		return c.status
	}
	return newStatusUpdater(c.db, c.logger, c.config)
}

func (c *ClaimManager) markSent(ctx context.Context, id int64, lockedAt *time.Time) {
	c.statusUpdater().markSentIfLocked(ctx, id, lockedAt)
}

func (c *ClaimManager) markFailed(ctx context.Context, id int64, lockedAt *time.Time, errMsg string) {
	c.statusUpdater().markFailedIfLocked(ctx, id, lockedAt, errMsg)
}

func (c *ClaimManager) outboxGrouper() *OutboxGrouper {
	if c == nil {
		return newOutboxGrouper(nil, nil, nil, Config{})
	}
	if c.grouper != nil {
		return c.grouper
	}
	return newOutboxGrouper(c.db, nil, c.logger, c.config)
}

func (c *ClaimManager) collectRoomsByChannel(ctx context.Context, items []domain.YouTubeNotificationOutbox) map[string]channelAlarmRoomTargets {
	return c.outboxGrouper().collectRoomsByChannel(ctx, items)
}

func (c *ClaimManager) filterLiveCatchupSuppressedRooms(
	ctx context.Context,
	item domain.YouTubeNotificationOutbox,
	rooms map[string]bool,
) map[string]bool {
	return c.outboxGrouper().filterLiveCatchupSuppressedRooms(ctx, item, rooms)
}

func (c *ClaimManager) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) deliveryDispatchResult {
	if c == nil || c.executor == nil {
		return deliveryDispatchResult{
			successDeliveryIDs: make([]int64, 0, len(rows)),
			touchedOutboxIDs:   make([]int64, 0, len(rows)),
			successClaimTokens: make([]deliveryClaimToken, 0, len(rows)),
			failureBuckets:     make(map[string][]int64),
		}
	}
	return c.executor.dispatchDeliveryRows(ctx, rows, outboxByID)
}

func (c *ClaimManager) recordDeliveryFailure(
	result *deliveryDispatchResult,
	mu *sync.Mutex,
	reason string,
	deliveryID, outboxID int64,
) {
	if c != nil && c.metrics != nil {
		c.metrics.recordDeliveryFailure(result, mu, reason, deliveryID, outboxID)
		return
	}
	mu.Lock()
	result.failedDeliveries++
	result.failureBuckets[reason] = append(result.failureBuckets[reason], deliveryID)
	result.touchedOutboxIDs = append(result.touchedOutboxIDs, outboxID)
	mu.Unlock()
}
