package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

func (d *ClaimManager) tryClaimDelivery(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
) (claimResult, error) {
	if shouldSkipDeliveryClaim(d, outbox) {
		return claimResult{decision: deliveryClaimDecisionProceed}, nil
	}

	repository := observation.NewRepositoryContext(ctx, d.db)
	// lease 시각은 event/schedule metadata와 분리한다. 과거·미래 재시도 시각으로 lock 수명을 바꾸면 안 된다.
	claimAt := time.Now().UTC().Truncate(time.Microsecond)

	postID, err := ytcontentid.ResolveDeliveryLogicalID(outbox.Kind, outbox.ContentID, outbox.Payload)
	if err != nil {
		return claimResult{decision: deliveryClaimDecisionRetryLater}, fmt.Errorf("resolve post id: %w", err)
	}

	state, err := repository.FindAlarmStateByPostID(ctx, outbox.Kind, postID)
	if err != nil {
		return claimResult{decision: deliveryClaimDecisionRetryLater}, fmt.Errorf("find alarm state by post id: %w", err)
	}

	alreadyCompleted, err := d.isCommunityShortsDeliveryAlreadyCompleted(ctx, repository, outbox, state)
	if err != nil {
		return claimResult{decision: deliveryClaimDecisionRetryLater}, fmt.Errorf("is community shorts delivery already completed: %w", err)
	}

	if alreadyCompleted {
		return claimResult{decision: deliveryClaimDecisionAlreadySent}, nil
	}

	refreshed, err := d.refreshStaleAlarmStateClaim(ctx, repository, outbox, postID, state, claimAt)
	if err != nil {
		return claimResult{decision: deliveryClaimDecisionRetryLater}, fmt.Errorf("refresh stale alarm state claim: %w", err)
	}

	if refreshed.done {
		return claimResult{decision: refreshed.decision}, nil
	}

	acquired, err := d.acquireAlarmStateClaim(ctx, repository, row, outbox, postID, refreshed.state, claimAt)
	if err != nil {
		return acquired, fmt.Errorf("acquire alarm state claim: %w", err)
	}

	return acquired, nil
}

func shouldSkipDeliveryClaim(d *ClaimManager, outbox *domain.YouTubeNotificationOutbox) bool {
	if outbox == nil {
		return true
	}

	return d == nil || reflectutil.IsNil(d.db) || !telemetry.IsCommunityShortsDeliveryAuditKind(outbox.Kind)
}

func deliveryClaimIdentityForOutbox(outbox *domain.YouTubeNotificationOutbox) (string, error) {
	if outbox == nil {
		return "", nil
	}

	if !telemetry.IsCommunityShortsDeliveryAuditKind(outbox.Kind) {
		return "", nil
	}

	postID, err := ytcontentid.ResolveDeliveryLogicalID(outbox.Kind, outbox.ContentID, outbox.Payload)
	if err != nil {
		return "", fmt.Errorf("resolve post id: %w", err)
	}

	return store.DeliveryClaimIdentityKey(outbox.Kind, postID), nil
}

func (d *ClaimManager) isCommunityShortsDeliveryAlreadyCompleted(
	ctx context.Context,
	repository *observation.PgxRepository,
	outbox *domain.YouTubeNotificationOutbox,
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
	repository *observation.PgxRepository,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	postID string,
	state *domain.YouTubeCommunityShortsAlarmState,
	claimAt time.Time,
) (*domain.YouTubeCommunityShortsAlarmState, error) {
	var trackingRow *domain.YouTubeContentAlarmTracking

	if claimNeedsTrackingRow(state) {
		loaded, err := d.loadClaimTrackingRow(ctx, repository, outbox)
		if err != nil {
			return nil, fmt.Errorf("load claim tracking row: %w", err)
		}

		trackingRow = loaded
	}

	contentID := resolveClaimContentID(outbox, state, trackingRow)
	channelID := resolveClaimChannelID(outbox, state, trackingRow)

	if channelID == "" {
		return nil, errors.New("build alarm state claim record: channel id is empty")
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

type staleClaimRefresh struct {
	state    *domain.YouTubeCommunityShortsAlarmState
	decision deliveryClaimDecision
	done     bool
}

func (d *ClaimManager) refreshStaleAlarmStateClaim(
	ctx context.Context,
	repository *observation.PgxRepository,
	outbox *domain.YouTubeNotificationOutbox,
	postID string,
	state *domain.YouTubeCommunityShortsAlarmState,
	claimAt time.Time,
) (staleClaimRefresh, error) {
	if state == nil {
		return staleClaimRefresh{decision: deliveryClaimDecisionProceed}, nil
	}

	if !isStaleAlarmStateClaim(state, claimAt, d.deliveryClaimTimeout()) {
		return staleClaimRefresh{state: state, decision: deliveryClaimDecisionProceed}, nil
	}

	if _, err := repository.ReleaseAlarmStateClaim(ctx, outbox.Kind, postID, *state.AuthorizedAt); err != nil {
		return staleClaimRefresh{decision: deliveryClaimDecisionRetryLater}, fmt.Errorf("release stale alarm state claim: %w", err)
	}

	reloadedState, alreadyCompleted, err := d.reloadAlarmStateClaimStatus(ctx, repository, outbox, postID, "reload alarm state by post id")
	if err != nil {
		return staleClaimRefresh{decision: deliveryClaimDecisionRetryLater}, fmt.Errorf("reload alarm state claim status: %w", err)
	}

	if alreadyCompleted {
		return staleClaimRefresh{state: reloadedState, decision: deliveryClaimDecisionAlreadySent, done: true}, nil
	}

	return staleClaimRefresh{state: reloadedState, decision: deliveryClaimDecisionProceed}, nil
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
	repository *observation.PgxRepository,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
	postID string,
	state *domain.YouTubeCommunityShortsAlarmState,
	claimAt time.Time,
) (claimResult, error) {
	claimRecord, err := d.buildAlarmStateClaimRecord(ctx, repository, row, outbox, postID, state, claimAt)
	if err != nil {
		return claimResult{decision: deliveryClaimDecisionRetryLater}, fmt.Errorf("build alarm state claim record: %w", err)
	}

	claimed, err := repository.TryClaimAlarmState(ctx, claimRecord)
	if err != nil {
		return claimResult{decision: deliveryClaimDecisionRetryLater}, fmt.Errorf("try claim alarm state: %w", err)
	}

	if claimed {
		success, successErr := d.finalizeClaimSuccess(ctx, repository, outbox, postID, claimAt)
		if successErr != nil {
			return success, fmt.Errorf("finalize claim success: %w", successErr)
		}

		return success, nil
	}

	miss, missErr := d.finalizeClaimMiss(ctx, repository, outbox, postID)
	if missErr != nil {
		return miss, fmt.Errorf("finalize claim miss: %w", missErr)
	}

	return miss, nil
}
