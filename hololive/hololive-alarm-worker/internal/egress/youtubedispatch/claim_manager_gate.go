package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/claim"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
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

type deliveryClaimSelection struct {
	sendRows               []domain.YouTubeNotificationDelivery
	sendOutboxes           []domain.YouTubeNotificationOutbox
	claimTokens            []dispatchstate.ClaimToken
	rowClaimTokens         [][]dispatchstate.ClaimToken
	alreadySentDeliveryIDs []int64
	alreadySentOutboxIDs   []int64
	alreadySentRows        []domain.YouTubeNotificationDelivery
	alreadySentOutboxes    []domain.YouTubeNotificationOutbox
	retryDeliveryIDs       []int64
	retryOutboxIDs         []int64
	retryRows              []domain.YouTubeNotificationDelivery
	retryOutboxes          []domain.YouTubeNotificationOutbox
	deferredDeliveryIDs    []int64
}

func (d *ClaimManager) selectClaimedDeliveries(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	reuseCache claim.DecisionCache,
) deliveryClaimSelection {
	safeRows := make([]domain.YouTubeNotificationDelivery, len(rows))
	copy(safeRows, rows)

	safeOutboxes := make([]domain.YouTubeNotificationOutbox, len(outboxes))
	copy(safeOutboxes, outboxes)

	selection := deliveryClaimSelection{
		sendRows:       make([]domain.YouTubeNotificationDelivery, 0, len(safeRows)),
		sendOutboxes:   make([]domain.YouTubeNotificationOutbox, 0, len(safeOutboxes)),
		claimTokens:    make([]dispatchstate.ClaimToken, 0, len(safeOutboxes)),
		rowClaimTokens: make([][]dispatchstate.ClaimToken, 0, len(safeRows)),
	}
	limit := min(len(safeOutboxes), len(safeRows))

	if limit == 0 {
		return selection
	}

	for i := range limit {
		d.applyDeliveryClaimSelection(ctx, &selection, &safeRows[i], &safeOutboxes[i], reuseCache)
	}

	return selection
}

func (d *ClaimManager) applyDeliveryClaimSelection(
	ctx context.Context,
	selection *deliveryClaimSelection,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	reuseCache claim.DecisionCache,
) {
	if err := validateDeliveryLogicalIdentity(row, outbox); err != nil {
		d.retryDeliveryClaimSelection(selection, row, outbox, "Failed to resolve delivery logical identity before send", err)

		return
	}

	claimIdentity, err := deliveryClaimIdentityForOutbox(outbox)
	if err != nil {
		d.retryDeliveryClaimSelection(selection, row, outbox, "Failed to resolve community/shorts delivery claim identity before send", err)

		return
	}

	result, err := reuseCache.ResolveClaim(ctx, claimIdentity, d.claimDeliveryResolver(row, outbox))
	if err != nil {
		d.retryDeliveryClaimSelection(selection, row, outbox, "Failed to claim community/shorts alarm state before send", err)

		return
	}

	cr, ok := result.Decision.Value.(claimResult)
	if !ok {
		d.retryDeliveryClaimSelection(selection, row, outbox, "Unexpected community/shorts claim result type before send", fmt.Errorf("claim result type %T", result.Decision.Value))

		return
	}

	// post 단위 결정은 reuseCache로 공유되지만 "이 room이 이미 받았는가"는 행마다 다르므로 캐시 밖에서 판정한다.
	decision := cr.decision
	if decision == deliveryClaimDecisionAlreadySent {
		roomDecision, roomErr := d.resolveRoomDeliveryDecision(ctx, row, outbox)
		if roomErr != nil {
			d.retryDeliveryClaimSelection(selection, row, outbox, "Failed to resolve per-room community/shorts sent state before send", roomErr)

			return
		}

		decision = roomDecision
	}

	d.applyDeliveryClaimDecision(ctx, selection, row, outbox, decision, cr.claimToken, result.Hit)
}

func validateDeliveryLogicalIdentity(
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
) error {
	if row == nil {
		return errors.New("delivery row is nil")
	}

	if outbox == nil {
		return errors.New("outbox row is nil")
	}

	if _, err := ytcontentid.ResolveDeliveryKey(outbox.Kind, outbox.ContentID, outbox.Payload, row.RoomID); err != nil {
		return fmt.Errorf("resolve delivery key: %w", err)
	}

	return nil
}

type claimResult struct {
	decision   deliveryClaimDecision
	claimToken *dispatchstate.ClaimToken
}

func (d *ClaimManager) claimDeliveryResolver(
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
) claim.ComputeFn {
	return func(ctx context.Context) (claim.ComputeResult, error) {
		resolved, claimErr := d.tryClaimDelivery(ctx, row, outbox)
		if claimErr != nil {
			return claim.ComputeResult{}, fmt.Errorf("try claim delivery: %w", claimErr)
		}

		var token *claim.Token

		if resolved.claimToken != nil {
			token = &claim.Token{AuthorizedAt: resolved.claimToken.AuthorizedAt}
		}

		return claim.ComputeResult{Decision: claim.Decision{Value: resolved}, Token: token}, nil
	}
}

func (d *ClaimManager) resolveRoomDeliveryDecision(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
) (deliveryClaimDecision, error) {
	received, err := d.roomAlreadyReceivedPost(ctx, row, outbox)
	if err != nil {
		return deliveryClaimDecisionRetryLater, fmt.Errorf("room already received post: %w", err)
	}

	if received {
		return deliveryClaimDecisionAlreadySent, nil
	}

	d.logClaimIssue("Proceeding with community/shorts delivery because this room has not received the already-sent post", row, outbox, slog.LevelInfo)

	return deliveryClaimDecisionProceed, nil
}

func (d *ClaimManager) retryDeliveryClaimSelection(
	selection *deliveryClaimSelection,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	message string,
	err error,
) {
	d.logClaimIssue(message, row, outbox, slog.LevelWarn, slog.Any("error", err))

	selection.retryDeliveryIDs = append(selection.retryDeliveryIDs, row.ID)
	selection.retryOutboxIDs = append(selection.retryOutboxIDs, outbox.ID)
	selection.retryRows = append(selection.retryRows, *row)
	selection.retryOutboxes = append(selection.retryOutboxes, *outbox)
}

func (d *ClaimManager) applyDeliveryClaimDecision(
	ctx context.Context,
	selection *deliveryClaimSelection,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	decision deliveryClaimDecision,
	claimToken *dispatchstate.ClaimToken,
	reused bool,
) {
	switch decision {
	case deliveryClaimDecisionProceed:
		appendProceedingDeliveryClaim(selection, row, outbox, claimToken, reused)
	case deliveryClaimDecisionAlreadySent:
		d.logClaimIssue("Skipped community/shorts delivery because the post was already sent", row, outbox, slog.LevelInfo)

		selection.alreadySentDeliveryIDs = append(selection.alreadySentDeliveryIDs, row.ID)
		selection.alreadySentOutboxIDs = append(selection.alreadySentOutboxIDs, outbox.ID)
		selection.alreadySentRows = append(selection.alreadySentRows, *row)
		selection.alreadySentOutboxes = append(selection.alreadySentOutboxes, *outbox)
	case deliveryClaimDecisionRetryLater:
		d.applyRetryLaterDeliveryClaim(ctx, selection, row, outbox)
	}
}

func (d *ClaimManager) applyRetryLaterDeliveryClaim(
	ctx context.Context,
	selection *deliveryClaimSelection,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
) {
	d.logClaimIssue("Skipped community/shorts delivery because another execution owns the post claim", row, outbox, slog.LevelInfo)

	deferred, err := d.deferDeliveryClaim(ctx, row)
	if err != nil {
		d.logClaimIssue("Failed to defer community/shorts delivery without consuming an attempt", row, outbox, slog.LevelWarn, slog.Any("error", err))
	}

	if !deferred {
		selection.retryDeliveryIDs = append(selection.retryDeliveryIDs, row.ID)
		selection.retryOutboxIDs = append(selection.retryOutboxIDs, outbox.ID)
		selection.retryRows = append(selection.retryRows, *row)
		selection.retryOutboxes = append(selection.retryOutboxes, *outbox)

		return
	}

	selection.deferredDeliveryIDs = append(selection.deferredDeliveryIDs, row.ID)
}

func (d *ClaimManager) deferDeliveryClaim(ctx context.Context, row *domain.YouTubeNotificationDelivery) (bool, error) {
	if d == nil || d.transition == nil || row == nil {
		return false, nil
	}

	nextAttemptAt := time.Now().UTC()

	if d.config.RetryBackoff > 0 {
		nextAttemptAt = nextAttemptAt.Add(d.config.RetryBackoff)
	}

	result, err := d.transition.DeferFollower(ctx, store.DeferCommand{
		Delivery: *row, NextAttemptAt: nextAttemptAt,
	})
	observeLifecycleApply("defer_follower", result, err, 1)

	if err != nil {
		return false, fmt.Errorf("defer version-fenced delivery claim: %w", err)
	}

	return result.Outcome == store.ApplyApplied, nil
}

func appendProceedingDeliveryClaim(
	selection *deliveryClaimSelection,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	claimToken *dispatchstate.ClaimToken,
	reused bool,
) {
	rowClaimTokens := []dispatchstate.ClaimToken(nil)

	if claimToken != nil && !reused {
		token := *claimToken

		selection.claimTokens = append(selection.claimTokens, token)
		rowClaimTokens = []dispatchstate.ClaimToken{token}
	}

	selection.sendRows = append(selection.sendRows, *row)
	selection.sendOutboxes = append(selection.sendOutboxes, *outbox)
	selection.rowClaimTokens = append(selection.rowClaimTokens, rowClaimTokens)
}
