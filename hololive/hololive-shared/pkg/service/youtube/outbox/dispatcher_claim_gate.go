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
		claimIdentity, identityErr := deliveryClaimIdentityForOutbox(outboxes[i])
		if identityErr != nil {
			d.logClaimIssue(
				"Failed to resolve community/shorts delivery claim identity before send",
				rows[i],
				outboxes[i],
				slog.LevelWarn,
				slog.Any("error", identityErr),
			)
			selection.retryDeliveryIDs = append(selection.retryDeliveryIDs, rows[i].ID)
			selection.retryOutboxIDs = append(selection.retryOutboxIDs, outboxes[i].ID)
			continue
		}
		decision, claimToken, reused, err := reuseCache.resolve(claimIdentity, func() (deliveryClaimDecision, *deliveryClaimToken, error) {
			return d.tryClaimDelivery(ctx, rows[i], outboxes[i])
		})
		if err != nil {
			d.logClaimIssue("Failed to claim community/shorts alarm state before send", rows[i], outboxes[i], slog.LevelWarn, slog.Any("error", err))
			selection.retryDeliveryIDs = append(selection.retryDeliveryIDs, rows[i].ID)
			selection.retryOutboxIDs = append(selection.retryOutboxIDs, outboxes[i].ID)
			continue
		}

		switch decision {
		case deliveryClaimDecisionProceed:
			rowClaimTokens := []deliveryClaimToken(nil)
			if claimToken != nil && !reused {
				token := *claimToken
				selection.claimTokens = append(selection.claimTokens, token)
				rowClaimTokens = []deliveryClaimToken{token}
			}
			selection.sendRows = append(selection.sendRows, rows[i])
			selection.sendOutboxes = append(selection.sendOutboxes, outboxes[i])
			selection.rowClaimTokens = append(selection.rowClaimTokens, rowClaimTokens)
		case deliveryClaimDecisionAlreadySent:
			d.logClaimIssue("Skipped community/shorts delivery because the post was already sent", rows[i], outboxes[i], slog.LevelInfo)
			selection.alreadySentDeliveryIDs = append(selection.alreadySentDeliveryIDs, rows[i].ID)
			selection.alreadySentOutboxIDs = append(selection.alreadySentOutboxIDs, outboxes[i].ID)
		case deliveryClaimDecisionRetryLater:
			d.logClaimIssue("Skipped community/shorts delivery because another execution owns the post claim", rows[i], outboxes[i], slog.LevelInfo)
			selection.retryDeliveryIDs = append(selection.retryDeliveryIDs, rows[i].ID)
			selection.retryOutboxIDs = append(selection.retryOutboxIDs, outboxes[i].ID)
		}
	}

	return selection
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
