package polling

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type publishedAtResolverRepository struct {
	db              *gorm.DB
	batchRepository *gormBatchRepository
}

type publishedAtFinalizeResult struct {
	enqueued bool
	reason   string
}

type publishedAtFinalizeEligibility struct {
	enqueuable bool
	reason     string
}

type resolvedPublishedAtDispatchGap struct {
	candidate   trackingrepo.PublishedAtResolutionCandidate
	publishedAt time.Time
}

type resolvedPublishedAtDispatchGapRow struct {
	Kind              domain.OutboxKind `gorm:"column:kind"`
	PostID            string            `gorm:"column:post_id"`
	ContentID         string            `gorm:"column:content_id"`
	ChannelID         string            `gorm:"column:channel_id"`
	DetectedAt        time.Time         `gorm:"column:detected_at"`
	ActualPublishedAt time.Time         `gorm:"column:actual_published_at"`
}

const (
	publishedAtClaimFreshWindow              = 30 * time.Second
	resolvedPublishedAtDispatchGapRecoverFor = time.Hour
)

func newPublishedAtResolverRepository(db *gorm.DB) *publishedAtResolverRepository {
	return &publishedAtResolverRepository{
		db:              db,
		batchRepository: &gormBatchRepository{db: db},
	}
}

func (r *publishedAtResolverRepository) FinalizePublishedAtAndMaybeEnqueue(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
) (publishedAtFinalizeResult, error) {
	if r == nil || r.db == nil {
		return publishedAtFinalizeResult{}, fmt.Errorf("finalize published_at: db is nil")
	}
	if publishedAt.IsZero() {
		return publishedAtFinalizeResult{}, fmt.Errorf("finalize published_at: published_at is empty")
	}

	normalizedPublishedAt := yttimestamp.Normalize(publishedAt)
	result := publishedAtFinalizeResult{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepository := trackingrepo.NewRepository(tx)
		eligibility, err := r.loadFinalizeEligibility(ctx, txRepository, candidate)
		if err != nil {
			return err
		}

		notification, reason, err := r.finalizeCandidateState(
			ctx,
			tx,
			txRepository,
			candidate,
			normalizedPublishedAt,
			routeDecider,
			eligibility.enqueuable,
		)
		if err != nil {
			return err
		}
		result.reason = selectPublishedAtFinalizeReason(eligibility.reason, reason)

		return r.completeFinalizePublishedAt(ctx, tx, txRepository, candidate, notification, &result)
	})
	if err != nil {
		return publishedAtFinalizeResult{}, fmt.Errorf("finalize published_at transaction: %w", err)
	}
	return result, nil
}

func (r *publishedAtResolverRepository) finalizeCandidateState(
	ctx context.Context,
	tx *gorm.DB,
	txRepository *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
	enqueueAllowed bool,
) (*domain.YouTubeNotificationOutbox, string, error) {
	switch candidate.Kind {
	case domain.OutboxKindNewShort:
		return r.finalizeShort(ctx, tx, txRepository, candidate, publishedAt, routeDecider, enqueueAllowed)
	case domain.OutboxKindCommunityPost:
		return r.finalizeCommunity(ctx, tx, txRepository, candidate, publishedAt, routeDecider, enqueueAllowed)
	default:
		return nil, "", fmt.Errorf("finalize published_at: unsupported kind %s", candidate.Kind)
	}
}

func (r *publishedAtResolverRepository) loadFinalizeEligibility(
	ctx context.Context,
	txRepository *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (publishedAtFinalizeEligibility, error) {
	eligibility := publishedAtFinalizeEligibility{enqueuable: true}

	stateEligibility, resolved, err := r.loadFinalizeAlarmStateEligibility(ctx, txRepository, candidate)
	if err != nil {
		return publishedAtFinalizeEligibility{}, err
	}
	if resolved {
		return stateEligibility, nil
	}

	trackingEligibility, resolved, err := loadFinalizeTrackingEligibility(ctx, txRepository, candidate)
	if err != nil {
		return publishedAtFinalizeEligibility{}, err
	}
	if resolved {
		return trackingEligibility, nil
	}

	return eligibility, nil
}

func (r *publishedAtResolverRepository) loadFinalizeAlarmStateEligibility(
	ctx context.Context,
	txRepository *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (publishedAtFinalizeEligibility, bool, error) {
	stateRow, err := txRepository.FindAlarmStateByPostID(ctx, candidate.Kind, candidate.PostID)
	if err != nil {
		return publishedAtFinalizeEligibility{}, false, fmt.Errorf("load alarm state row: %w", err)
	}
	if stateRow == nil {
		return publishedAtFinalizeEligibility{}, false, nil
	}
	if isPublishedAtAlarmStateSent(stateRow) {
		return publishedAtFinalizeEligibility{reason: "already_sent"}, true, nil
	}
	if !isPublishedAtAlarmStateClaimed(stateRow) {
		return publishedAtFinalizeEligibility{}, false, nil
	}

	return r.resolveFinalizeAlarmStateClaimEligibility(ctx, txRepository, candidate, *stateRow.AuthorizedAt)
}

func isPublishedAtAlarmStateSent(stateRow *domain.YouTubeCommunityShortsAlarmState) bool {
	return stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero()
}

func isPublishedAtAlarmStateClaimed(stateRow *domain.YouTubeCommunityShortsAlarmState) bool {
	return stateRow.AuthorizedAt != nil && !stateRow.AuthorizedAt.IsZero()
}

func (r *publishedAtResolverRepository) resolveFinalizeAlarmStateClaimEligibility(
	ctx context.Context,
	txRepository *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	authorizedAt time.Time,
) (publishedAtFinalizeEligibility, bool, error) {
	if isPublishedAtClaimFresh(authorizedAt) {
		return publishedAtFinalizeEligibility{reason: "already_claimed"}, true, nil
	}

	exists, err := r.outboxExistsForCandidate(ctx, txRepository, candidate)
	if err != nil {
		return publishedAtFinalizeEligibility{}, false, err
	}
	if exists {
		return publishedAtFinalizeEligibility{reason: "already_claimed"}, true, nil
	}

	released, err := txRepository.ReleaseAlarmStateClaim(ctx, candidate.Kind, candidate.PostID, authorizedAt)
	if err != nil {
		return publishedAtFinalizeEligibility{}, false, fmt.Errorf("release stale alarm state claim: %w", err)
	}
	if !released {
		return publishedAtFinalizeEligibility{reason: "already_claimed"}, true, nil
	}

	return publishedAtFinalizeEligibility{}, false, nil
}

func loadFinalizeTrackingEligibility(
	ctx context.Context,
	txRepository *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (publishedAtFinalizeEligibility, bool, error) {
	trackingRow, err := txRepository.FindByIdentity(ctx, candidate.Kind, candidate.ContentID)
	if err != nil {
		return publishedAtFinalizeEligibility{}, false, fmt.Errorf("load tracking row: %w", err)
	}
	if isPublishedAtTrackingSent(trackingRow) {
		return publishedAtFinalizeEligibility{reason: "already_sent"}, true, nil
	}

	return publishedAtFinalizeEligibility{}, false, nil
}

func isPublishedAtTrackingSent(trackingRow *domain.YouTubeContentAlarmTracking) bool {
	return trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero()
}

func isPublishedAtClaimFresh(authorizedAt time.Time) bool {
	if authorizedAt.IsZero() {
		return false
	}

	return time.Since(authorizedAt.UTC()) < publishedAtClaimFreshWindow
}

func (r *publishedAtResolverRepository) outboxExistsForCandidate(
	ctx context.Context,
	txRepository *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (bool, error) {
	trackingRow, err := txRepository.FindByIdentity(ctx, candidate.Kind, candidate.ContentID)
	if err != nil {
		return false, fmt.Errorf("load tracking row: %w", err)
	}
	if trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero() {
		return true, nil
	}

	var count int64
	if err := r.db.WithContext(ctx).
		Model(&domain.YouTubeNotificationOutbox{}).
		Where("kind = ? AND content_id = ?", candidate.Kind, candidate.ContentID).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("load outbox row: %w", err)
	}

	return count > 0, nil
}

func selectPublishedAtFinalizeReason(primary string, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func (r *publishedAtResolverRepository) ListResolvedPublishedAtDispatchGaps(ctx context.Context, referenceNow time.Time, detectedBefore time.Time, limit int) ([]resolvedPublishedAtDispatchGap, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list resolved published_at dispatch gaps: db is nil")
	}
	if err := validateResolvedPublishedAtDispatchGapRequest(referenceNow, detectedBefore, limit); err != nil {
		return nil, err
	}

	var rows []resolvedPublishedAtDispatchGapRow
	if err := r.db.WithContext(ctx).
		Table("youtube_community_shorts_alarm_states AS s").
		Select("s.kind, s.post_id, s.content_id, s.channel_id, s.detected_at, s.actual_published_at").
		Joins("LEFT JOIN youtube_content_alarm_tracking AS t ON t.kind = s.kind AND t.canonical_content_id = s.post_id").
		Where("s.kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
		Where("s.actual_published_at IS NOT NULL").
		Where("s.alarm_sent_at IS NULL").
		Where("t.alarm_sent_at IS NULL").
		Where("s.detected_at < ?", yttimestamp.Normalize(detectedBefore)).
		Where("s.actual_published_at >= ?", yttimestamp.Normalize(referenceNow).Add(-resolvedPublishedAtDispatchGapRecoverFor)).
		Where("s.published_at_retry_after IS NULL OR s.published_at_retry_after <= ?", yttimestamp.Normalize(referenceNow)).
		Where(`NOT EXISTS (
			SELECT 1
			FROM youtube_notification_outbox AS o
			WHERE o.kind = s.kind AND (o.content_id = s.content_id OR o.content_id = s.post_id)
		)`).
		Order("s.detected_at ASC").
		Order("s.post_id ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list resolved published_at dispatch gaps: query rows: %w", err)
	}

	return buildResolvedPublishedAtDispatchGaps(rows), nil
}

func validateResolvedPublishedAtDispatchGapRequest(referenceNow time.Time, detectedBefore time.Time, limit int) error {
	if referenceNow.IsZero() {
		return fmt.Errorf("list resolved published_at dispatch gaps: reference now is empty")
	}
	if detectedBefore.IsZero() {
		return fmt.Errorf("list resolved published_at dispatch gaps: detected before is empty")
	}
	if limit <= 0 {
		return fmt.Errorf("list resolved published_at dispatch gaps: limit must be positive")
	}

	return nil
}

func buildResolvedPublishedAtDispatchGaps(rows []resolvedPublishedAtDispatchGapRow) []resolvedPublishedAtDispatchGap {
	gaps := make([]resolvedPublishedAtDispatchGap, 0, len(rows))
	for i := range rows {
		postID := normalizeContentID(rows[i].Kind, rows[i].PostID)
		contentID := normalizeContentID(rows[i].Kind, rows[i].ContentID)
		if postID == "" || contentID == "" {
			continue
		}
		gaps = append(gaps, resolvedPublishedAtDispatchGap{
			candidate: trackingrepo.PublishedAtResolutionCandidate{
				Kind:       rows[i].Kind,
				PostID:     postID,
				ContentID:  contentID,
				ChannelID:  strings.TrimSpace(rows[i].ChannelID),
				DetectedAt: yttimestamp.Normalize(rows[i].DetectedAt),
			},
			publishedAt: yttimestamp.Normalize(rows[i].ActualPublishedAt),
		})
	}

	return gaps
}
