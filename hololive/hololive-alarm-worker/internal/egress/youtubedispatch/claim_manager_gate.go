package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
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
	members             []claimedDelivery
	retryMembers        []claimedDelivery
	deferredDeliveryIDs []int64
}

func (d *ClaimManager) selectClaimedDeliveries(
	ctx context.Context,
	rows []domain.YouTubeNotificationDelivery,
	outboxes []domain.YouTubeNotificationOutbox,
	reuseCache *claimDecisionCache,
) deliveryClaimSelection {
	limit := min(len(outboxes), len(rows))
	selection := deliveryClaimSelection{
		members: make([]claimedDelivery, 0, limit),
	}

	if limit == 0 {
		return selection
	}

	for i := range rows {
		if i >= len(outboxes) {
			break
		}

		d.applyDeliveryClaimSelection(ctx, &selection, &rows[i], &outboxes[i], reuseCache)
	}

	return selection
}

func (d *ClaimManager) applyDeliveryClaimSelection(
	ctx context.Context,
	selection *deliveryClaimSelection,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	reuseCache *claimDecisionCache,
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

	d.applyDeliveryClaimDecision(ctx, selection, row, outbox, result.Decision.decision, result.Decision.claimToken, result.Hit)
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
) claimDecisionCompute {
	return func(ctx context.Context) (claimResult, error) {
		resolved, err := d.tryClaimDelivery(ctx, row, outbox)
		if err != nil {
			return claimResult{}, fmt.Errorf("try claim delivery: %w", err)
		}

		return resolved, nil
	}
}

func (d *ClaimManager) retryDeliveryClaimSelection(
	selection *deliveryClaimSelection,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	message string,
	err error,
) {
	d.logClaimIssue(message, row, outbox, slog.LevelWarn, slog.Any("error", err))

	selection.retryMembers = append(selection.retryMembers, claimedDelivery{row: *row, outbox: *outbox})
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
		// post 단위 SENT는 다른 room의 발송일 수 있다. 이 room의 SENT 원장은 PrepareClaimed가 이미 걸렀고,
		// 그 뒤 기록된 SENT는 BeginSending이 원장을 잠가 conflict로 막으므로 claim token 없이 진행한다.
		d.logClaimIssue("Proceeding with community/shorts delivery because this room has no sent ledger entry", row, outbox, slog.LevelInfo)
		appendProceedingDeliveryClaim(selection, row, outbox, nil, reused)
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
		selection.retryMembers = append(selection.retryMembers, claimedDelivery{row: *row, outbox: *outbox})

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
	member := claimedDelivery{row: *row, outbox: *outbox}

	if claimToken != nil {
		token := *claimToken

		token.Reused = reused
		// 다른 room이 먼저 끝나도 같은 epoch의 완료 증명이 필요하다. 해제 소유권과 구분한다.
		member.claimTokens = []dispatchstate.ClaimToken{token}
	}

	selection.members = append(selection.members, member)
}
