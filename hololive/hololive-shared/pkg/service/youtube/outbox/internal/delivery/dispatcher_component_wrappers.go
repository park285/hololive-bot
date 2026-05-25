package delivery

import (
	"context"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache/claim"
)

func (d *Dispatcher) claimManager() *ClaimManager {
	if d == nil {
		return newClaimManager(nil, nil, Config{}, nil, nil, nil, nil, nil)
	}
	if d.claim != nil {
		return d.claim
	}
	claimManager := newClaimManager(nil, d.logger, d.config, nil, nil, d.status, d.grouper, d.audit)
	claimManager.setMetricsRecorder(d.metrics)
	return claimManager
}

func (d *Dispatcher) sendEngine() *SendEngine {
	if d == nil {
		return newSendEngine(nil, nil, nil, Config{}, nil, nil, nil)
	}
	if d.send != nil {
		return d.send
	}
	return newSendEngine(nil, nil, d.logger, d.config, d.claimManager(), d.audit, d.metrics)
}

func (d *Dispatcher) enqueueDeliveries(ctx context.Context, outboxItems []domain.YouTubeNotificationOutbox, roomsByChannel map[string]channelAlarmRoomTargets) {
	d.claimManager().enqueueDeliveries(ctx, outboxItems, roomsByChannel)
}

func (d *Dispatcher) processPendingDeliveries(ctx context.Context) int {
	return d.claimManager().processPendingDeliveries(ctx)
}

func (d *Dispatcher) dispatchDeliveryRows(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) deliveryDispatchResult {
	return d.sendEngine().dispatchDeliveryRows(ctx, rows, outboxByID)
}

func (d *Dispatcher) sendDeliveryMessage(ctx context.Context, req deliverySendRequest) error {
	return d.sendEngine().sendDeliveryMessage(ctx, req)
}

func (d *Dispatcher) dispatchClaimedRowsIndividually(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	formattedMessages map[int64]string,
	formatFailures map[int64]bool,
	rowClaimTokens [][]deliveryClaimToken,
	result *deliveryDispatchResult,
	mu *sync.Mutex,
) {
	d.sendEngine().dispatchClaimedRowsIndividually(ctx, rows, outboxes, formattedMessages, formatFailures, rowClaimTokens, result, mu)
}

func (d *Dispatcher) selectClaimedDeliveries(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	reuseCache claim.DecisionCache,
) deliveryClaimSelection {
	return d.claimManager().selectClaimedDeliveries(ctx, rows, outboxes, reuseCache)
}
