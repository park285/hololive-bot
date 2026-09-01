package youtubedispatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
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
	claimAt := resolveDeliveryClaimTime(row, outbox)
	postID := strings.TrimSpace(telemetry.ResolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload))

	if postID == "" {
		return claimResult{decision: deliveryClaimDecisionRetryLater}, errors.New("resolve post id: empty")
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

	return d == nil || deliverysql.IsNilDB(d.db) || !telemetry.IsCommunityShortsDeliveryAuditKind(outbox.Kind)
}

func resolveDeliveryClaimTime(row *domain.YouTubeNotificationDelivery, outbox *domain.YouTubeNotificationOutbox) time.Time {
	for _, candidate := range deliveryClaimTimeCandidates(row, outbox) {
		if !candidate.IsZero() {
			return normalizeDeliveryClaimTime(candidate)
		}
	}

	return normalizeDeliveryClaimTime(time.Now())
}

func deliveryClaimTimeCandidates(row *domain.YouTubeNotificationDelivery, outbox *domain.YouTubeNotificationOutbox) []time.Time {
	if outbox == nil {
		return []time.Time{time.Now()}
	}

	return []time.Time{
		outbox.NextAttemptAt,
		deliveryRowCreatedAt(row),
		outbox.CreatedAt,
		time.Now(),
	}
}

func deliveryRowCreatedAt(row *domain.YouTubeNotificationDelivery) time.Time {
	if row == nil {
		return time.Time{}
	}

	return row.CreatedAt
}

func normalizeDeliveryClaimTime(value time.Time) time.Time {
	return yttimestamp.Normalize(value).Truncate(time.Microsecond)
}

func deliveryClaimIdentityForOutbox(outbox *domain.YouTubeNotificationOutbox) (string, error) {
	if outbox == nil {
		return "", nil
	}

	if !telemetry.IsCommunityShortsDeliveryAuditKind(outbox.Kind) {
		return "", nil
	}

	postID := strings.TrimSpace(telemetry.ResolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload))
	if postID == "" {
		return "", errors.New("resolve post id: empty")
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

func (d *ClaimManager) roomAlreadyReceivedPost(
	ctx context.Context,
	row *domain.YouTubeNotificationDelivery,
	outbox *domain.YouTubeNotificationOutbox,
) (bool, error) {
	if row == nil || shouldSkipDeliveryClaim(d, outbox) {
		return false, nil
	}

	postID := strings.TrimSpace(telemetry.ResolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload))
	if postID == "" {
		return false, errors.New("resolve post id: empty")
	}

	rows, err := d.db.Query(ctx, mustSQL("dispatcher_claim_acquire_0131_01.sql"), string(outbox.Kind), outbox.ContentID, postID, row.RoomID, string(domain.OutboxStatusSent), row.ID)
	if err != nil {
		return false, fmt.Errorf("load sent sibling deliveries for room: %w", err)
	}
	defer rows.Close()

	out, err := sentSiblingRowsContainPost(rows, outbox.Kind, postID)
	if err != nil {
		return out, fmt.Errorf("sent sibling rows contain post: %w", err)
	}

	return out, nil
}

func sentSiblingRowsContainPost(rows pgx.Rows, kind domain.OutboxKind, postID string) (bool, error) {
	for rows.Next() {
		var contentID, payload string

		if err := rows.Scan(&contentID, &payload); err != nil {
			return false, fmt.Errorf("scan sent sibling delivery for room: %w", err)
		}

		if strings.TrimSpace(telemetry.ResolveTelemetryPostID(kind, contentID, payload)) == postID {
			return true, nil
		}
	}

	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate sent sibling deliveries for room: %w", err)
	}

	return false, nil
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
