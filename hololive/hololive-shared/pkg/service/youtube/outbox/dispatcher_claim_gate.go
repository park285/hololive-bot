package outbox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	deliveryFailureReasonPreSendClaim = "pre-send claim"
	maxCommunityShortsClaimHold       = 2 * time.Minute
)

type deliveryClaimDecision int

const (
	deliveryClaimDecisionProceed deliveryClaimDecision = iota
	deliveryClaimDecisionAlreadySent
	deliveryClaimDecisionRetryLater
)

type deliveryClaimToken struct {
	kind         domain.OutboxKind
	postID       string
	authorizedAt time.Time
}

type deliveryClaimSelection struct {
	sendRows               []domain.YouTubeNotificationDelivery
	sendOutboxes           []domain.YouTubeNotificationOutbox
	claimTokens            []deliveryClaimToken
	rowClaimTokens         [][]deliveryClaimToken
	alreadySentDeliveryIDs []int64
	alreadySentOutboxIDs   []int64
	retryDeliveryIDs       []int64
	retryOutboxIDs         []int64
}

type deliveryClaimReuse struct {
	decision deliveryClaimDecision
}

type deliveryClaimReuseCache struct {
	mu      sync.Mutex
	entries map[string]deliveryClaimReuse
}

func (d *Dispatcher) selectClaimedDeliveries(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	reuseCache *deliveryClaimReuseCache,
) deliveryClaimSelection {
	selection := deliveryClaimSelection{
		sendRows:       make([]domain.YouTubeNotificationDelivery, 0, len(rows)),
		sendOutboxes:   make([]domain.YouTubeNotificationOutbox, 0, len(outboxes)),
		claimTokens:    make([]deliveryClaimToken, 0, len(outboxes)),
		rowClaimTokens: make([][]deliveryClaimToken, 0, len(rows)),
	}
	limit := min(len(outboxes), len(rows))

	for i := range limit {
		d.applyDeliveryClaimSelection(ctx, &selection, rows[i], outboxes[i], reuseCache)
	}

	return selection
}

func (d *Dispatcher) applyDeliveryClaimSelection(
	ctx context.Context,
	selection *deliveryClaimSelection,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	reuseCache *deliveryClaimReuseCache,
) {
	claimIdentity, err := deliveryClaimIdentityForOutbox(outbox)
	if err != nil {
		d.retryDeliveryClaimSelection(selection, row, outbox, "Failed to resolve community/shorts delivery claim identity before send", err)
		return
	}

	decision, claimToken, reused, err := reuseCache.resolve(claimIdentity, func() (deliveryClaimDecision, *deliveryClaimToken, error) {
		return d.tryClaimDelivery(ctx, row, outbox)
	})
	if err != nil {
		d.retryDeliveryClaimSelection(selection, row, outbox, "Failed to claim community/shorts alarm state before send", err)
		return
	}

	d.applyDeliveryClaimDecision(selection, row, outbox, decision, claimToken, reused)
}

func (d *Dispatcher) retryDeliveryClaimSelection(
	selection *deliveryClaimSelection,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	message string,
	err error,
) {
	d.logClaimIssue(message, row, outbox, slog.LevelWarn, slog.Any("error", err))
	selection.retryDeliveryIDs = append(selection.retryDeliveryIDs, row.ID)
	selection.retryOutboxIDs = append(selection.retryOutboxIDs, outbox.ID)
}

func (d *Dispatcher) applyDeliveryClaimDecision(
	selection *deliveryClaimSelection,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	decision deliveryClaimDecision,
	claimToken *deliveryClaimToken,
	reused bool,
) {
	switch decision {
	case deliveryClaimDecisionProceed:
		appendProceedingDeliveryClaim(selection, row, outbox, claimToken, reused)
	case deliveryClaimDecisionAlreadySent:
		d.logClaimIssue("Skipped community/shorts delivery because the post was already sent", row, outbox, slog.LevelInfo)
		selection.alreadySentDeliveryIDs = append(selection.alreadySentDeliveryIDs, row.ID)
		selection.alreadySentOutboxIDs = append(selection.alreadySentOutboxIDs, outbox.ID)
	case deliveryClaimDecisionRetryLater:
		d.logClaimIssue("Skipped community/shorts delivery because another execution owns the post claim", row, outbox, slog.LevelInfo)
		selection.retryDeliveryIDs = append(selection.retryDeliveryIDs, row.ID)
		selection.retryOutboxIDs = append(selection.retryOutboxIDs, outbox.ID)
	}
}

func appendProceedingDeliveryClaim(
	selection *deliveryClaimSelection,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	claimToken *deliveryClaimToken,
	reused bool,
) {
	rowClaimTokens := []deliveryClaimToken(nil)
	if claimToken != nil && !reused {
		token := *claimToken
		selection.claimTokens = append(selection.claimTokens, token)
		rowClaimTokens = []deliveryClaimToken{token}
	}
	selection.sendRows = append(selection.sendRows, row)
	selection.sendOutboxes = append(selection.sendOutboxes, outbox)
	selection.rowClaimTokens = append(selection.rowClaimTokens, rowClaimTokens)
}

func newDeliveryClaimReuseCache(capacity int) *deliveryClaimReuseCache {
	if capacity < 1 {
		capacity = 1
	}
	return &deliveryClaimReuseCache{
		entries: make(map[string]deliveryClaimReuse, capacity),
	}
}

func (c *deliveryClaimReuseCache) resolve(
	identity string,
	compute func() (deliveryClaimDecision, *deliveryClaimToken, error),
) (deliveryClaimDecision, *deliveryClaimToken, bool, error) {
	if identity == "" {
		decision, token, err := compute()
		return decision, token, false, err
	}
	if c == nil {
		decision, token, err := compute()
		return decision, token, false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if reuse, ok := c.entries[identity]; ok {
		return reuse.decision, nil, true, nil
	}

	decision, token, err := compute()
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, false, err
	}

	c.entries[identity] = deliveryClaimReuse{decision: decision}
	return decision, token, false, nil
}

func (d *Dispatcher) applyClaimSelection(result *deliveryDispatchResult, mu *sync.Mutex, selection deliveryClaimSelection) {
	if len(selection.alreadySentDeliveryIDs) > 0 {
		mu.Lock()
		result.successDeliveryIDs = append(result.successDeliveryIDs, selection.alreadySentDeliveryIDs...)
		result.touchedOutboxIDs = append(result.touchedOutboxIDs, selection.alreadySentOutboxIDs...)
		mu.Unlock()
	}

	for i := range selection.retryDeliveryIDs {
		outboxID := int64(0)
		if i < len(selection.retryOutboxIDs) {
			outboxID = selection.retryOutboxIDs[i]
		}
		d.recordDeliveryFailure(result, mu, deliveryFailureReasonPreSendClaim, selection.retryDeliveryIDs[i], outboxID)
	}
}
