package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
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
		sendRows:     make([]domain.YouTubeNotificationDelivery, 0, len(rows)),
		sendOutboxes: make([]domain.YouTubeNotificationOutbox, 0, len(outboxes)),
		claimTokens:  make([]deliveryClaimToken, 0, len(outboxes)),
	}
	limit := len(rows)
	if len(outboxes) < limit {
		limit = len(outboxes)
	}

	for i := 0; i < limit; i++ {
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
			selection.sendRows = append(selection.sendRows, rows[i])
			selection.sendOutboxes = append(selection.sendOutboxes, outboxes[i])
			if claimToken != nil && !reused {
				selection.claimTokens = append(selection.claimTokens, *claimToken)
			}
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

func (d *Dispatcher) tryClaimDelivery(
	ctx context.Context,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
) (deliveryClaimDecision, *deliveryClaimToken, error) {
	if d == nil || d.db == nil || !isCommunityShortsDeliveryAuditKind(outbox.Kind) {
		return deliveryClaimDecisionProceed, nil, nil
	}

	repo := trackingrepo.NewRepository(d.db)
	claimAt := resolveDeliveryClaimTime(row, outbox)
	postID := strings.TrimSpace(resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload))
	if postID == "" {
		return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("resolve post id: empty")
	}

	state, err := repo.FindAlarmStateByPostID(ctx, outbox.Kind, postID)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("find alarm state by post id: %w", err)
	}
	alreadyCompleted, err := d.isCommunityShortsDeliveryAlreadyCompleted(ctx, repo, outbox, state)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, err
	}
	if alreadyCompleted {
		return deliveryClaimDecisionAlreadySent, nil, nil
	}
	if state != nil && state.AuthorizedAt != nil && !state.AuthorizedAt.IsZero() && state.AuthorizedAt.UTC().Before(claimAt.Add(-d.deliveryClaimTimeout())) {
		if _, err := repo.ReleaseAlarmStateClaim(ctx, outbox.Kind, postID, *state.AuthorizedAt); err != nil {
			return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("release stale alarm state claim: %w", err)
		}
		state, err = repo.FindAlarmStateByPostID(ctx, outbox.Kind, postID)
		if err != nil {
			return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("reload alarm state by post id: %w", err)
		}
		alreadyCompleted, err = d.isCommunityShortsDeliveryAlreadyCompleted(ctx, repo, outbox, state)
		if err != nil {
			return deliveryClaimDecisionRetryLater, nil, err
		}
		if alreadyCompleted {
			return deliveryClaimDecisionAlreadySent, nil, nil
		}
	}

	claimRecord, err := d.buildAlarmStateClaimRecord(ctx, repo, row, outbox, postID, state, claimAt)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, err
	}

	claimed, err := repo.TryClaimAlarmState(ctx, claimRecord)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("try claim alarm state: %w", err)
	}
	if claimed {
		state, err = repo.FindAlarmStateByPostID(ctx, outbox.Kind, postID)
		if err != nil {
			return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("reload alarm state after claim success: %w", err)
		}
		if communityShortsAlarmStateMarkedSent(state) {
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

	state, err = repo.FindAlarmStateByPostID(ctx, outbox.Kind, postID)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, fmt.Errorf("reload alarm state after claim miss: %w", err)
	}
	alreadyCompleted, err = d.isCommunityShortsDeliveryAlreadyCompleted(ctx, repo, outbox, state)
	if err != nil {
		return deliveryClaimDecisionRetryLater, nil, err
	}
	if alreadyCompleted {
		return deliveryClaimDecisionAlreadySent, nil, nil
	}

	return deliveryClaimDecisionRetryLater, nil, nil
}

func resolveDeliveryClaimTime(row domain.YouTubeNotificationDelivery, outbox domain.YouTubeNotificationOutbox) time.Time {
	switch {
	case !outbox.NextAttemptAt.IsZero():
		return yttimestamp.Normalize(outbox.NextAttemptAt)
	case !row.CreatedAt.IsZero():
		return yttimestamp.Normalize(row.CreatedAt)
	case !outbox.CreatedAt.IsZero():
		return yttimestamp.Normalize(outbox.CreatedAt)
	default:
		return yttimestamp.Normalize(time.Now())
	}
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

func (d *Dispatcher) isCommunityShortsDeliveryAlreadyCompleted(
	ctx context.Context,
	repo *trackingrepo.GormRepository,
	outbox domain.YouTubeNotificationOutbox,
	state *domain.YouTubeCommunityShortsAlarmState,
) (bool, error) {
	if communityShortsAlarmStateMarkedSent(state) {
		return true, nil
	}

	trackingRow, err := repo.FindByIdentity(ctx, outbox.Kind, outbox.ContentID)
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

func (d *Dispatcher) buildAlarmStateClaimRecord(
	ctx context.Context,
	repo *trackingrepo.GormRepository,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	postID string,
	state *domain.YouTubeCommunityShortsAlarmState,
	claimAt time.Time,
) (*domain.YouTubeCommunityShortsAlarmState, error) {
	needTrackingRow := state == nil ||
		strings.TrimSpace(state.ContentID) == "" ||
		strings.TrimSpace(state.ChannelID) == "" ||
		state.DetectedAt.IsZero() ||
		state.ActualPublishedAt == nil

	var trackingRow *domain.YouTubeContentAlarmTracking
	var err error
	if needTrackingRow {
		trackingRow, err = repo.FindByIdentity(ctx, outbox.Kind, outbox.ContentID)
		if err != nil {
			return nil, fmt.Errorf("load tracking row: %w", err)
		}
	}

	contentID := strings.TrimSpace(outbox.ContentID)
	if state != nil && strings.TrimSpace(state.ContentID) != "" {
		contentID = strings.TrimSpace(state.ContentID)
	} else if trackingRow != nil && strings.TrimSpace(trackingRow.ContentID) != "" {
		contentID = strings.TrimSpace(trackingRow.ContentID)
	}
	channelID := strings.TrimSpace(outbox.ChannelID)
	if state != nil && strings.TrimSpace(state.ChannelID) != "" {
		channelID = strings.TrimSpace(state.ChannelID)
	} else if trackingRow != nil && strings.TrimSpace(trackingRow.ChannelID) != "" {
		channelID = strings.TrimSpace(trackingRow.ChannelID)
	}
	if channelID == "" {
		return nil, fmt.Errorf("build alarm state claim record: channel id is empty")
	}

	actualPublishedAt := resolveClaimActualPublishedAt(state, trackingRow, outbox)
	detectedAt := claimAt
	switch {
	case state != nil && !state.DetectedAt.IsZero():
		detectedAt = state.DetectedAt
	case trackingRow != nil && !trackingRow.DetectedAt.IsZero():
		detectedAt = trackingRow.DetectedAt
	case !outbox.CreatedAt.IsZero():
		detectedAt = outbox.CreatedAt
	case !row.CreatedAt.IsZero():
		detectedAt = row.CreatedAt
	}
	detectedAt = yttimestamp.Normalize(detectedAt)
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
		var payload videoPayload
		if err := json.Unmarshal([]byte(outbox.Payload), &payload); err == nil {
			return yttimestamp.NormalizePtr(payload.PublishedAt)
		}
	case domain.OutboxKindCommunityPost:
		var payload communityPayload
		if err := json.Unmarshal([]byte(outbox.Payload), &payload); err == nil {
			return yttimestamp.NormalizePtr(payload.PublishedAt)
		}
	}
	return nil
}

func claimTokensForIndex(claimTokens []deliveryClaimToken, idx int, total int) []deliveryClaimToken {
	if len(claimTokens) == 0 {
		return nil
	}
	if len(claimTokens) == total && idx >= 0 && idx < len(claimTokens) {
		return []deliveryClaimToken{claimTokens[idx]}
	}
	return claimTokens
}

func (d *Dispatcher) releaseDeliveryClaims(ctx context.Context, claims []deliveryClaimToken) error {
	if d == nil || d.db == nil || len(claims) == 0 {
		return nil
	}

	repo := trackingrepo.NewRepository(d.db)
	for i := range claims {
		if _, err := repo.ReleaseAlarmStateClaim(ctx, claims[i].kind, claims[i].postID, claims[i].authorizedAt); err != nil {
			return fmt.Errorf("release claim at index %d: %w", i, err)
		}
	}
	return nil
}

func (d *Dispatcher) deliveryClaimTimeout() time.Duration {
	claimTimeout := maxCommunityShortsClaimHold
	if d != nil && d.cfg.LockTimeout > 0 && d.cfg.LockTimeout < claimTimeout {
		claimTimeout = d.cfg.LockTimeout
	}
	if claimTimeout <= 0 {
		return maxCommunityShortsClaimHold
	}
	return claimTimeout
}

func (d *Dispatcher) logClaimIssue(
	message string,
	row domain.YouTubeNotificationDelivery,
	outbox domain.YouTubeNotificationOutbox,
	level slog.Level,
	attrs ...any,
) {
	if d == nil || d.logger == nil {
		return
	}

	baseAttrs := []any{
		slog.Int64(logschema.FieldDeliveryID, row.ID),
		slog.Int64(logschema.FieldOutboxID, outbox.ID),
		slog.String(logschema.FieldRoomID, row.RoomID),
		slog.String(logschema.FieldChannelID, outbox.ChannelID),
		slog.String(deliveryAuditPostIDLogField, resolveTelemetryPostID(outbox.Kind, outbox.ContentID, outbox.Payload)),
		slog.String(deliveryAuditContentIDLogField, strings.TrimSpace(outbox.ContentID)),
		slog.String(deliveryAuditAlarmTypeLogField, string(outbox.Kind.ToAlarmType())),
	}
	baseAttrs = append(baseAttrs, attrs...)

	switch level {
	case slog.LevelWarn:
		d.logger.Warn(message, baseAttrs...)
	case slog.LevelError:
		d.logger.Error(message, baseAttrs...)
	default:
		d.logger.Info(message, baseAttrs...)
	}
}
