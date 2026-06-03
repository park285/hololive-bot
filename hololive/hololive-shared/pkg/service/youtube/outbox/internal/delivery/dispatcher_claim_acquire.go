package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func (d *ClaimManager) tryClaimDelivery(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
) (deliveryClaimDecision, *deliveryClaimToken, error) {
	if shouldSkipDeliveryClaim(d, outbox) {
		return deliveryClaimDecisionProceed, nil, nil
	}

	repository := trackingrepo.NewRepository(d.db)
	claimAt := resolveDeliveryClaimTime(row, outbox)
	postID := strings.TrimSpace(resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload))
	if postID == "" {
		return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("resolve post id: empty")
	}

	state, err := repository.FindAlarmStateByPostID(ctx, outbox.Kind, postID)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("find alarm state by post id: %w", err)
	}

	alreadyCompleted, err := d.isCommunityShortsDeliveryAlreadyCompleted(ctx, repository, outbox, state)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, err
	}

	if alreadyCompleted {
		return deliveryClaimDecisionAlreadySent, nil, nil
	}

	state, decision, done, err := d.refreshStaleAlarmStateClaim(ctx, repository, outbox, postID, state, claimAt)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, err
	}
	if done {
		return decision, nil, nil
	}

	return d.acquireAlarmStateClaim(ctx, repository, row, outbox, postID, state, claimAt)
}

func shouldSkipDeliveryClaim(d *ClaimManager, outbox domain.YouTubeNotificationOutbox) bool {
	return d == nil || deliverysql.IsNilDB(d.db) || !isCommunityShortsDeliveryAuditKind(outbox.Kind)
}

func resolveDeliveryClaimTime(row domain.YouTubeNotificationDelivery, outbox domain.YouTubeNotificationOutbox) time.Time {
	switch {
	case !outbox.NextAttemptAt.IsZero():
		return normalizeDeliveryClaimTime(outbox.NextAttemptAt)
	case !row.CreatedAt.IsZero():
		return normalizeDeliveryClaimTime(row.CreatedAt)
	case !outbox.CreatedAt.IsZero():
		return normalizeDeliveryClaimTime(outbox.CreatedAt)
	default:
		return normalizeDeliveryClaimTime(time.Now())
	}
}

func normalizeDeliveryClaimTime(value time.Time) time.Time {
	return yttimestamp.Normalize(value).Truncate(time.Microsecond)
}

func deliveryClaimIdentityForOutbox(outbox domain.YouTubeNotificationOutbox) (string, error) {
	if !isCommunityShortsDeliveryAuditKind(outbox.Kind) {
		return "", nil
	}

	postID := strings.TrimSpace(resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload))
	if postID == "" {
		return "", fmt.Errorf("resolve post id: empty")
	}

	return deliveryClaimIdentityKey(outbox.Kind, postID), nil
}

func (d *ClaimManager) isCommunityShortsDeliveryAlreadyCompleted(
	ctx context.Context,
	repository *trackingrepo.PgxRepository,
	outbox domain.YouTubeNotificationOutbox,
	state *domain.YouTubeCommunityShortsAlarmState,
) (bool, error) {
	if communityShortsAlarmStateMarkedSent(state) {
		return true, nil
	}

	trackingRow, err := repository.FindByIdentity(ctx, outbox.Kind, outbox.ContentID)
	if err != nil {
		return false, fmt.Errorf("load tracking row: %w", err)
	}
	return communityShortsTrackingRowMarkedSent(trackingRow), nil
}

func communityShortsAlarmStateMarkedSent(state *domain.YouTubeCommunityShortsAlarmState) bool {
	return state != nil && state.AlarmSentAt != nil && !state.AlarmSentAt.IsZero()
}

func communityShortsTrackingRowMarkedSent(row *domain.YouTubeContentAlarmTracking) bool {
	return row != nil && row.AlarmSentAt != nil && !row.AlarmSentAt.IsZero()
}

func (d *ClaimManager) buildAlarmStateClaimRecord(
	ctx context.Context,
	repository *trackingrepo.PgxRepository,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	postID string,
	state *domain.YouTubeCommunityShortsAlarmState,
	claimAt time.Time,
) (*domain.YouTubeCommunityShortsAlarmState, error) {
	trackingRow, err := d.loadClaimTrackingRow(ctx, repository, outbox, state)
	if err != nil {
		return nil, err
	}

	contentID := resolveClaimContentID(outbox, state, trackingRow)
	channelID := resolveClaimChannelID(outbox, state, trackingRow)
	if channelID == "" {
		return nil, fmt.Errorf("build alarm state claim record: channel id is empty")
	}

	actualPublishedAt := resolveClaimActualPublishedAt(state, trackingRow, outbox)
	detectedAt := resolveClaimDetectedAt(row, outbox, state, trackingRow, claimAt)
	authorizedAt := claimAt

	return &domain.YouTubeCommunityShortsAlarmState{
		Kind:              outbox.Kind,
		PostID:            postID,
		ContentID:         contentID,
		ChannelID:         channelID,
		ActualPublishedAt: actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}, nil
}

func (d *ClaimManager) refreshStaleAlarmStateClaim(
	ctx context.Context,
	repository *trackingrepo.PgxRepository,
	outbox domain.YouTubeNotificationOutbox,
	postID string,
	state *domain.YouTubeCommunityShortsAlarmState,
	claimAt time.Time,
) (*domain.YouTubeCommunityShortsAlarmState, deliveryClaimDecision, bool, error) {
	if !isStaleAlarmStateClaim(state, claimAt, d.deliveryClaimTimeout()) {
		return state, deliveryClaimDecisionProceed, false, nil
	}

	if _, err := repository.ReleaseAlarmStateClaim(ctx, outbox.Kind, postID, *state.AuthorizedAt); err != nil {
		return nil, deliveryClaimDecisionRetryLater, false, fmt.Errorf("release stale alarm state claim: %w", err)
	}

	reloadedState, alreadyCompleted, err := d.reloadAlarmStateClaimStatus(ctx, repository, outbox, postID, "reload alarm state by post id")
	if err != nil {
		return nil, deliveryClaimDecisionRetryLater, false, err
	}
	if alreadyCompleted {
		return reloadedState, deliveryClaimDecisionAlreadySent, true, nil
	}

	return reloadedState, deliveryClaimDecisionProceed, false, nil
}

func isStaleAlarmStateClaim(
	state *domain.YouTubeCommunityShortsAlarmState,
	claimAt time.Time,
	claimTimeout time.Duration,
) bool {
	return state != nil &&
		state.AuthorizedAt != nil &&
		!state.AuthorizedAt.IsZero() &&
		state.AuthorizedAt.UTC().Before(claimAt.Add(-claimTimeout))
}

func (d *ClaimManager) acquireAlarmStateClaim(
	ctx context.Context,
	repository *trackingrepo.PgxRepository,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	postID string,
	state *domain.YouTubeCommunityShortsAlarmState,
	claimAt time.Time,
) (deliveryClaimDecision, *deliveryClaimToken, error) {
	claimRecord, err := d.buildAlarmStateClaimRecord(ctx, repository, row, outbox, postID, state, claimAt)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, err
	}

	claimed, err := repository.TryClaimAlarmState(ctx, claimRecord)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("try claim alarm state: %w", err)
	}
	if claimed {
		return d.finalizeClaimSuccess(ctx, repository, outbox, postID, claimAt)
	}

	return d.finalizeClaimMiss(ctx, repository, outbox, postID)
}
