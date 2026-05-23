package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	json "github.com/park285/hololive-bot/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func (d *Dispatcher) finalizeClaimSuccess(
	ctx context.Context,
	repository *trackingrepo.GormRepository,
	outbox domain.YouTubeNotificationOutbox,
	postID string,
	claimAt time.Time,
) (deliveryClaimDecision, *deliveryClaimToken, error) {
	state, alreadyCompleted, err := d.reloadAlarmStateClaimStatus(ctx, repository, outbox, postID, "reload alarm state after claim success")
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, err
	}
	if alreadyCompleted {
		return deliveryClaimDecisionAlreadySent, nil, nil
	}
	if state == nil || state.AuthorizedAt == nil || !state.AuthorizedAt.UTC().Equal(claimAt) {
		return deliveryClaimDecisionRetryLater, nil, nil
	}

	return deliveryClaimDecisionProceed, &deliveryClaimToken{
		kind:         outbox.Kind,
		postID:       postID,
		authorizedAt: claimAt,
	}, nil
}

func (d *Dispatcher) finalizeClaimMiss(
	ctx context.Context,
	repository *trackingrepo.GormRepository,
	outbox domain.YouTubeNotificationOutbox,
	postID string,
) (deliveryClaimDecision, *deliveryClaimToken, error) {
	_, alreadyCompleted, err := d.reloadAlarmStateClaimStatus(ctx, repository, outbox, postID, "reload alarm state after claim miss")
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, err
	}
	if alreadyCompleted {
		return deliveryClaimDecisionAlreadySent, nil, nil
	}

	return deliveryClaimDecisionRetryLater, nil, nil
}

func (d *Dispatcher) reloadAlarmStateClaimStatus(
	ctx context.Context,
	repository *trackingrepo.GormRepository,
	outbox domain.YouTubeNotificationOutbox,
	postID string,
	action string,
) (*domain.YouTubeCommunityShortsAlarmState, bool, error) {
	state, err := repository.FindAlarmStateByPostID(ctx, outbox.Kind, postID)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", action, err)
	}

	alreadyCompleted, err := d.isCommunityShortsDeliveryAlreadyCompleted(ctx, repository, outbox, state)
	if err != nil {
		return nil, false, err
	}

	return state, alreadyCompleted, nil
}

func (d *Dispatcher) loadClaimTrackingRow(
	ctx context.Context,
	repository *trackingrepo.GormRepository,
	outbox domain.YouTubeNotificationOutbox,
	state *domain.YouTubeCommunityShortsAlarmState,
) (*domain.YouTubeContentAlarmTracking, error) {
	if !claimNeedsTrackingRow(state) {
		return nil, nil
	}

	trackingRow, err := repository.FindByIdentity(ctx, outbox.Kind, outbox.ContentID)
	if err != nil {
		return nil, fmt.Errorf("load tracking row: %w", err)
	}

	return trackingRow, nil
}

func claimNeedsTrackingRow(state *domain.YouTubeCommunityShortsAlarmState) bool {
	return state == nil ||
		strings.TrimSpace(state.ContentID) == "" ||
		strings.TrimSpace(state.ChannelID) == "" ||
		state.DetectedAt.IsZero() ||
		state.ActualPublishedAt == nil
}

func resolveClaimContentID(
	outbox domain.YouTubeNotificationOutbox,
	state *domain.YouTubeCommunityShortsAlarmState,
	trackingRow *domain.YouTubeContentAlarmTracking,
) string {
	switch {
	case state != nil && strings.TrimSpace(state.ContentID) != "":
		return strings.TrimSpace(state.ContentID)
	case trackingRow != nil && strings.TrimSpace(trackingRow.ContentID) != "":
		return strings.TrimSpace(trackingRow.ContentID)
	default:
		return strings.TrimSpace(outbox.ContentID)
	}
}

func resolveClaimChannelID(
	outbox domain.YouTubeNotificationOutbox,
	state *domain.YouTubeCommunityShortsAlarmState,
	trackingRow *domain.YouTubeContentAlarmTracking,
) string {
	switch {
	case state != nil && strings.TrimSpace(state.ChannelID) != "":
		return strings.TrimSpace(state.ChannelID)
	case trackingRow != nil && strings.TrimSpace(trackingRow.ChannelID) != "":
		return strings.TrimSpace(trackingRow.ChannelID)
	default:
		return strings.TrimSpace(outbox.ChannelID)
	}
}

func resolveClaimDetectedAt(
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	state *domain.YouTubeCommunityShortsAlarmState,
	trackingRow *domain.YouTubeContentAlarmTracking,
	claimAt time.Time,
) time.Time {
	for _, candidate := range claimDetectedAtCandidates(row, outbox, state, trackingRow, claimAt) {
		if !candidate.IsZero() {
			return yttimestamp.Normalize(candidate)
		}
	}
	return yttimestamp.Normalize(claimAt)
}

func claimDetectedAtCandidates(
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	state *domain.YouTubeCommunityShortsAlarmState,
	trackingRow *domain.YouTubeContentAlarmTracking,
	claimAt time.Time,
) []time.Time {
	candidates := make([]time.Time, 0, 5)
	if state != nil {
		candidates = append(candidates, state.DetectedAt)
	}
	if trackingRow != nil {
		candidates = append(candidates, trackingRow.DetectedAt)
	}
	return append(candidates, outbox.CreatedAt, row.CreatedAt, claimAt)
}

func resolveClaimActualPublishedAt(
	state *domain.YouTubeCommunityShortsAlarmState,
	trackingRow *domain.YouTubeContentAlarmTracking,
	outbox domain.YouTubeNotificationOutbox,
) *time.Time {
	switch {
	case state != nil && state.ActualPublishedAt != nil:
		return cloneUTCTimePtr(state.ActualPublishedAt)
	case trackingRow != nil && trackingRow.ActualPublishedAt != nil:
		return cloneUTCTimePtr(trackingRow.ActualPublishedAt)
	default:
		return resolveOutboxPublishedAt(outbox)
	}
}

func resolveOutboxPublishedAt(outbox domain.YouTubeNotificationOutbox) *time.Time {
	switch outbox.Kind {
	case domain.OutboxKindNewShort, domain.OutboxKindNewVideo:
		return resolveVideoPayloadPublishedAt(outbox.Payload)
	case domain.OutboxKindCommunityPost:
		return resolveCommunityPayloadPublishedAt(outbox.Payload)
	}
	return nil
}

func resolveVideoPayloadPublishedAt(rawPayload string) *time.Time {
	var payload videoPayload
	if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
		return nil
	}
	return yttimestamp.NormalizePtr(payload.PublishedAt)
}

func resolveCommunityPayloadPublishedAt(rawPayload string) *time.Time {
	var payload communityPayload
	if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
		return nil
	}
	return yttimestamp.NormalizePtr(payload.PublishedAt)
}
