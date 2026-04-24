package poller

import (
	"context"
	"fmt"
	"strings"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type publishedAtResolverRepository struct {
	db        *gorm.DB
	batchRepo *gormBatchRepository
}

type publishedAtFinalizeResult struct {
	enqueued bool
	reason   string
}

type publishedAtFinalizeEligibility struct {
	enqueuable bool
	reason     string
}

const publishedAtClaimFreshWindow = 30 * time.Second

func newPublishedAtResolverRepository(db *gorm.DB) *publishedAtResolverRepository {
	return &publishedAtResolverRepository{
		db:        db,
		batchRepo: &gormBatchRepository{db: db},
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
		txRepo := trackingrepo.NewRepository(tx)
		eligibility, err := r.loadFinalizeEligibility(ctx, txRepo, candidate)
		if err != nil {
			return err
		}

		notification, reason, err := r.finalizeCandidateState(
			ctx,
			tx,
			txRepo,
			candidate,
			normalizedPublishedAt,
			routeDecider,
			eligibility.enqueuable,
		)
		if err != nil {
			return err
		}
		result.reason = selectPublishedAtFinalizeReason(eligibility.reason, reason)

		return r.completeFinalizePublishedAt(ctx, tx, txRepo, candidate, notification, &result)
	})
	if err != nil {
		return publishedAtFinalizeResult{}, fmt.Errorf("finalize published_at transaction: %w", err)
	}
	return result, nil
}

func (r *publishedAtResolverRepository) finalizeCandidateState(
	ctx context.Context,
	tx *gorm.DB,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
	enqueueAllowed bool,
) (*domain.YouTubeNotificationOutbox, string, error) {
	switch candidate.Kind {
	case domain.OutboxKindNewShort:
		return r.finalizeShort(ctx, tx, txRepo, candidate, publishedAt, routeDecider, enqueueAllowed)
	case domain.OutboxKindCommunityPost:
		return r.finalizeCommunity(ctx, tx, txRepo, candidate, publishedAt, routeDecider, enqueueAllowed)
	default:
		return nil, "", fmt.Errorf("finalize published_at: unsupported kind %s", candidate.Kind)
	}
}

func (r *publishedAtResolverRepository) loadFinalizeEligibility(
	ctx context.Context,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (publishedAtFinalizeEligibility, error) {
	eligibility := publishedAtFinalizeEligibility{enqueuable: true}

	stateRow, err := txRepo.FindAlarmStateByPostID(ctx, candidate.Kind, candidate.PostID)
	if err != nil {
		return publishedAtFinalizeEligibility{}, fmt.Errorf("load alarm state row: %w", err)
	}
	if stateRow != nil {
		if stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero() {
			return publishedAtFinalizeEligibility{reason: "already_sent"}, nil
		}
		if stateRow.AuthorizedAt != nil && !stateRow.AuthorizedAt.IsZero() {
			if isPublishedAtClaimFresh(*stateRow.AuthorizedAt) {
				return publishedAtFinalizeEligibility{reason: "already_claimed"}, nil
			}

			exists, err := r.outboxExistsForCandidate(ctx, txRepo, candidate)
			if err != nil {
				return publishedAtFinalizeEligibility{}, err
			}
			if exists {
				return publishedAtFinalizeEligibility{reason: "already_claimed"}, nil
			}

			released, err := txRepo.ReleaseAlarmStateClaim(ctx, candidate.Kind, candidate.PostID, *stateRow.AuthorizedAt)
			if err != nil {
				return publishedAtFinalizeEligibility{}, fmt.Errorf("release stale alarm state claim: %w", err)
			}
			if !released {
				return publishedAtFinalizeEligibility{reason: "already_claimed"}, nil
			}
		}
	}

	trackingRow, err := txRepo.FindByIdentity(ctx, candidate.Kind, candidate.ContentID)
	if err != nil {
		return publishedAtFinalizeEligibility{}, fmt.Errorf("load tracking row: %w", err)
	}
	if trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero() {
		return publishedAtFinalizeEligibility{reason: "already_sent"}, nil
	}

	return eligibility, nil
}

func isPublishedAtClaimFresh(authorizedAt time.Time) bool {
	if authorizedAt.IsZero() {
		return false
	}

	return time.Since(authorizedAt.UTC()) < publishedAtClaimFreshWindow
}

func (r *publishedAtResolverRepository) outboxExistsForCandidate(
	ctx context.Context,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (bool, error) {
	trackingRow, err := txRepo.FindByIdentity(ctx, candidate.Kind, candidate.ContentID)
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

func (r *publishedAtResolverRepository) completeFinalizePublishedAt(
	ctx context.Context,
	tx *gorm.DB,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	notification *domain.YouTubeNotificationOutbox,
	result *publishedAtFinalizeResult,
) error {
	if notification != nil {
		if err := r.batchRepo.batchInsertNotifications(ctx, tx, []*domain.YouTubeNotificationOutbox{notification}); err != nil {
			return fmt.Errorf("insert pending notification: %w", err)
		}
		result.enqueued = true
		if result.reason == "" {
			result.reason = "resolved"
		}
	}

	if err := txRepo.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID); err != nil {
		return fmt.Errorf("clear published_at retry after: %w", err)
	}

	return nil
}

func (r *publishedAtResolverRepository) finalizeShort(
	ctx context.Context,
	tx *gorm.DB,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
	enqueueAllowed bool,
) (*domain.YouTubeNotificationOutbox, string, error) {
	videoID, resolveErr := resolveShortFinalizeVideoID(candidate)
	if resolveErr != nil {
		return nil, "", resolveErr
	}
	if err := updateShortPublishedAt(ctx, tx, videoID, publishedAt); err != nil {
		return nil, "", err
	}
	if err := upsertResolvedPublishedAtState(ctx, txRepo, candidate, publishedAt, "short"); err != nil {
		return nil, "", err
	}

	video, err := loadShortNotificationVideo(ctx, tx, videoID)
	if err != nil {
		return nil, "", fmt.Errorf("load short row for notification: %w", err)
	}
	proceed, reason, err := maybeAuthorizePublishedAtNotification(
		ctx,
		txRepo,
		candidate,
		publishedAt,
		enqueueAllowed,
		routeDecider,
		domain.AlarmTypeShorts,
		"short",
	)
	if err != nil {
		return nil, "", err
	}
	if !proceed {
		return nil, reason, nil
	}

	return newShortPublishedAtNotification(candidate, video), "", nil
}

func (r *publishedAtResolverRepository) finalizeCommunity(
	ctx context.Context,
	tx *gorm.DB,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	routeDecider NotificationRouteDecider,
	enqueueAllowed bool,
) (*domain.YouTubeNotificationOutbox, string, error) {
	postID, resolveErr := resolveCommunityFinalizePostID(candidate)
	if resolveErr != nil {
		return nil, "", resolveErr
	}
	if err := updateCommunityPublishedAt(ctx, tx, postID, publishedAt); err != nil {
		return nil, "", err
	}
	if err := upsertResolvedPublishedAtState(ctx, txRepo, candidate, publishedAt, "community"); err != nil {
		return nil, "", err
	}

	post, err := loadCommunityNotificationPost(ctx, tx, postID)
	if err != nil {
		return nil, "", fmt.Errorf("load community row for notification: %w", err)
	}
	proceed, reason, err := maybeAuthorizePublishedAtNotification(
		ctx,
		txRepo,
		candidate,
		publishedAt,
		enqueueAllowed,
		routeDecider,
		domain.AlarmTypeCommunity,
		"community",
	)
	if err != nil {
		return nil, "", err
	}
	if !proceed {
		return nil, reason, nil
	}

	return newCommunityPublishedAtNotification(candidate, post), "", nil
}

func resolveShortFinalizeVideoID(candidate trackingrepo.PublishedAtResolutionCandidate) (string, error) {
	videoID := normalizeShortVideoResourceID(candidate.PostID)
	if videoID == "" {
		videoID = normalizeShortVideoResourceID(candidate.ContentID)
	}
	if videoID == "" {
		return "", fmt.Errorf("finalize short published_at: empty video id")
	}
	return videoID, nil
}

func resolveCommunityFinalizePostID(candidate trackingrepo.PublishedAtResolutionCandidate) (string, error) {
	postID := normalizeContentID(candidate.Kind, candidate.PostID)
	if postID == "" {
		postID = normalizeContentID(candidate.Kind, candidate.ContentID)
	}
	if postID == "" {
		return "", fmt.Errorf("finalize community published_at: empty post id")
	}
	return postID, nil
}

func updateShortPublishedAt(ctx context.Context, tx *gorm.DB, videoID string, publishedAt time.Time) error {
	result := tx.WithContext(ctx).
		Model(&domain.YouTubeVideo{}).
		Where("video_id = ?", videoID).
		Update("published_at", publishedAt)
	if result.Error != nil {
		return fmt.Errorf("update short published_at: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("update short published_at: video %s not found", videoID)
	}
	return nil
}

func updateCommunityPublishedAt(ctx context.Context, tx *gorm.DB, postID string, publishedAt time.Time) error {
	result := tx.WithContext(ctx).
		Model(&domain.YouTubeCommunityPost{}).
		Where("post_id = ?", postID).
		Update("published_at", publishedAt)
	if result.Error != nil {
		return fmt.Errorf("update community published_at: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("update community published_at: post %s not found", postID)
	}
	return nil
}

func upsertResolvedPublishedAtState(
	ctx context.Context,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	scope string,
) error {
	trackingRow := &domain.YouTubeContentAlarmTracking{
		Kind:              candidate.Kind,
		ContentID:         candidate.ContentID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}
	if err := txRepo.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{trackingRow}); err != nil {
		return fmt.Errorf("upsert %s tracking: %w", scope, err)
	}

	sourcePost := &domain.YouTubeCommunityShortsSourcePost{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}
	if err := txRepo.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{sourcePost}); err != nil {
		return fmt.Errorf("upsert %s source post: %w", scope, err)
	}

	alarmState := &domain.YouTubeCommunityShortsAlarmState{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ContentID:         candidate.ContentID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}
	if err := txRepo.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{alarmState}); err != nil {
		return fmt.Errorf("upsert %s alarm state: %w", scope, err)
	}

	return nil
}

func maybeAuthorizePublishedAtNotification(
	ctx context.Context,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	enqueueAllowed bool,
	routeDecider NotificationRouteDecider,
	alarmType domain.AlarmType,
	scope string,
) (bool, string, error) {
	if !enqueueAllowed {
		return false, "", nil
	}
	if !shouldEnqueueRoutedNotification(routeDecider, alarmType, candidate.ChannelID, publishedAt) {
		return false, "route_decider_rejected", nil
	}

	claimed, err := claimResolvedPublishedAtAlarmState(ctx, txRepo, candidate, publishedAt, scope)
	if err != nil {
		return false, "", err
	}
	if !claimed {
		return false, "already_claimed", nil
	}

	return true, "", nil
}

func claimResolvedPublishedAtAlarmState(
	ctx context.Context,
	txRepo *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	scope string,
) (bool, error) {
	authorizedAt := yttimestamp.Normalize(time.Now())
	claimed, err := txRepo.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ContentID:         candidate.ContentID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
		AuthorizedAt:      &authorizedAt,
	})
	if err != nil {
		return false, fmt.Errorf("claim %s alarm state: %w", scope, err)
	}
	return claimed, nil
}

func newShortPublishedAtNotification(
	candidate trackingrepo.PublishedAtResolutionCandidate,
	video *domain.YouTubeVideo,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   buildShortNotificationPayload(video, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}
}

func newCommunityPublishedAtNotification(
	candidate trackingrepo.PublishedAtResolutionCandidate,
	post *domain.YouTubeCommunityPost,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      candidate.Kind,
		ChannelID: candidate.ChannelID,
		ContentID: candidate.ContentID,
		Payload:   buildCommunityNotificationPayload(post, candidate.PostID),
		Status:    domain.OutboxStatusPending,
	}
}

type shortNotificationVideoRow struct {
	VideoID       string     `gorm:"column:video_id"`
	ChannelID     string     `gorm:"column:channel_id"`
	Title         string     `gorm:"column:title"`
	Thumbnail     string     `gorm:"column:thumbnail"`
	Duration      string     `gorm:"column:duration"`
	PublishedText string     `gorm:"column:published_text"`
	PublishedAt   *time.Time `gorm:"column:published_at"`
	IsShort       bool       `gorm:"column:is_short"`
	IsLiveReplay  bool       `gorm:"column:is_live_replay"`
	ViewCount     int64      `gorm:"column:view_count"`
	FirstSeenAt   time.Time  `gorm:"column:first_seen_at"`
	LastSeenAt    time.Time  `gorm:"column:last_seen_at"`
}

func loadShortNotificationVideo(ctx context.Context, tx *gorm.DB, videoID string) (*domain.YouTubeVideo, error) {
	var row shortNotificationVideoRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeVideo{}).
		Select("video_id, channel_id, title, thumbnail, duration, published_text, published_at, is_short, is_live_replay, view_count, first_seen_at, last_seen_at").
		Take(&row, "video_id = ?", videoID).Error; err != nil {
		return nil, err
	}

	thumbnail, err := parseThumbnailsJSON(row.Thumbnail)
	if err != nil {
		return nil, fmt.Errorf("parse short thumbnail: %w", err)
	}

	return &domain.YouTubeVideo{
		VideoID:       row.VideoID,
		ChannelID:     row.ChannelID,
		Title:         row.Title,
		Thumbnail:     thumbnail,
		Duration:      row.Duration,
		PublishedText: row.PublishedText,
		PublishedAt:   row.PublishedAt,
		IsShort:       row.IsShort,
		IsLiveReplay:  row.IsLiveReplay,
		ViewCount:     row.ViewCount,
		FirstSeenAt:   row.FirstSeenAt,
		LastSeenAt:    row.LastSeenAt,
	}, nil
}

type communityNotificationPostRow struct {
	PostID        string     `gorm:"column:post_id"`
	ChannelID     string     `gorm:"column:channel_id"`
	AuthorName    string     `gorm:"column:author_name"`
	AuthorPhoto   string     `gorm:"column:author_photo"`
	ContentText   string     `gorm:"column:content_text"`
	PublishedText string     `gorm:"column:published_text"`
	PublishedAt   *time.Time `gorm:"column:published_at"`
	LikeCount     int64      `gorm:"column:like_count"`
	CommentCount  int64      `gorm:"column:comment_count"`
	Images        string     `gorm:"column:images"`
	AttachedVideo string     `gorm:"column:attached_video"`
	FirstSeenAt   time.Time  `gorm:"column:first_seen_at"`
	LastSeenAt    time.Time  `gorm:"column:last_seen_at"`
}

func loadCommunityNotificationPost(ctx context.Context, tx *gorm.DB, postID string) (*domain.YouTubeCommunityPost, error) {
	var row communityNotificationPostRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeCommunityPost{}).
		Select("post_id, channel_id, author_name, author_photo, content_text, published_text, published_at, like_count, comment_count, images, attached_video, first_seen_at, last_seen_at").
		Take(&row, "post_id = ?", postID).Error; err != nil {
		return nil, err
	}

	authorPhoto, err := parseThumbnailsJSON(row.AuthorPhoto)
	if err != nil {
		return nil, fmt.Errorf("parse community author_photo: %w", err)
	}
	images, err := parseThumbnailsJSON(row.Images)
	if err != nil {
		return nil, fmt.Errorf("parse community images: %w", err)
	}

	return &domain.YouTubeCommunityPost{
		PostID:        row.PostID,
		ChannelID:     row.ChannelID,
		AuthorName:    row.AuthorName,
		AuthorPhoto:   authorPhoto,
		ContentText:   row.ContentText,
		PublishedText: row.PublishedText,
		PublishedAt:   row.PublishedAt,
		LikeCount:     row.LikeCount,
		CommentCount:  row.CommentCount,
		Images:        images,
		AttachedVideo: row.AttachedVideo,
		FirstSeenAt:   row.FirstSeenAt,
		LastSeenAt:    row.LastSeenAt,
	}, nil
}

func parseThumbnailsJSON(raw string) (domain.ThumbnailsJSON, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}

	var thumbnails domain.ThumbnailsJSON
	if err := json.Unmarshal([]byte(raw), &thumbnails); err != nil {
		return nil, fmt.Errorf("parse thumbnails json: %w", err)
	}
	return thumbnails, nil
}
