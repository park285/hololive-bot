package polling

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func (r *publishedAtResolverRepository) completeFinalizePublishedAt(
	ctx context.Context,
	tx dbx.Querier,
	txRepository *trackingrepo.PgxRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	notification *domain.YouTubeNotificationOutbox,
	result *publishedAtFinalizeResult,
) error {
	if notification != nil {
		if err := r.batchRepository.BatchInsertNotifications(ctx, tx, []*domain.YouTubeNotificationOutbox{notification}); err != nil {
			return fmt.Errorf("insert pending notification: %w", err)
		}
		result.enqueued = true
		if result.reason == "" {
			result.reason = "resolved"
		}
	}

	if err := txRepository.ClearPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID); err != nil {
		return fmt.Errorf("clear published_at retry after: %w", err)
	}

	return nil
}

func (r *publishedAtResolverRepository) finalizeShort(
	ctx context.Context,
	tx dbx.Querier,
	txRepository *trackingrepo.PgxRepository,
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
	if err := upsertResolvedPublishedAtState(ctx, txRepository, candidate, publishedAt, "short"); err != nil {
		return nil, "", err
	}

	video, err := loadShortNotificationVideo(ctx, tx, videoID)
	if err != nil {
		return nil, "", fmt.Errorf("load short row for notification: %w", err)
	}
	proceed, reason, err := maybeAuthorizePublishedAtNotification(
		ctx,
		txRepository,
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
	tx dbx.Querier,
	txRepository *trackingrepo.PgxRepository,
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
	if err := upsertResolvedPublishedAtState(ctx, txRepository, candidate, publishedAt, "community"); err != nil {
		return nil, "", err
	}

	post, err := loadCommunityNotificationPost(ctx, tx, postID)
	if err != nil {
		return nil, "", fmt.Errorf("load community row for notification: %w", err)
	}
	proceed, reason, err := maybeAuthorizePublishedAtNotification(
		ctx,
		txRepository,
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
	videoID := NormalizeShortVideoResourceID(candidate.PostID)
	if videoID == "" {
		videoID = NormalizeShortVideoResourceID(candidate.ContentID)
	}
	if videoID == "" {
		return "", fmt.Errorf("finalize short published_at: empty video id")
	}
	return videoID, nil
}

func resolveCommunityFinalizePostID(candidate trackingrepo.PublishedAtResolutionCandidate) (string, error) {
	postID := NormalizeContentID(candidate.Kind, candidate.PostID)
	if postID == "" {
		postID = NormalizeContentID(candidate.Kind, candidate.ContentID)
	}
	if postID == "" {
		return "", fmt.Errorf("finalize community published_at: empty post id")
	}
	return postID, nil
}

func updateShortPublishedAt(ctx context.Context, tx dbx.Querier, videoID string, publishedAt time.Time) error {
	tag, err := tx.Exec(ctx, `
		UPDATE youtube_videos
		SET published_at = $1
		WHERE video_id = $2`,
		publishedAt,
		videoID,
	)
	if err != nil {
		return fmt.Errorf("update short published_at: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update short published_at: video %s not found", videoID)
	}
	return nil
}

func updateCommunityPublishedAt(ctx context.Context, tx dbx.Querier, postID string, publishedAt time.Time) error {
	tag, err := tx.Exec(ctx, `
		UPDATE youtube_community_posts
		SET published_at = $1
		WHERE post_id = $2`,
		publishedAt,
		postID,
	)
	if err != nil {
		return fmt.Errorf("update community published_at: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update community published_at: post %s not found", postID)
	}
	return nil
}

func upsertResolvedPublishedAtState(
	ctx context.Context,
	txRepository *trackingrepo.PgxRepository,
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
	if err := txRepository.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{trackingRow}); err != nil {
		return fmt.Errorf("upsert %s tracking: %w", scope, err)
	}

	sourcePost := &domain.YouTubeCommunityShortsSourcePost{
		Kind:              candidate.Kind,
		PostID:            candidate.PostID,
		ChannelID:         candidate.ChannelID,
		ActualPublishedAt: &publishedAt,
		DetectedAt:        candidate.DetectedAt,
	}
	if err := txRepository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{sourcePost}); err != nil {
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
	if err := txRepository.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{alarmState}); err != nil {
		return fmt.Errorf("upsert %s alarm state: %w", scope, err)
	}

	return nil
}

func maybeAuthorizePublishedAtNotification(
	ctx context.Context,
	txRepository *trackingrepo.PgxRepository,
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
	if !ShouldEnqueueRoutedNotification(routeDecider, alarmType, candidate.ChannelID, publishedAt) {
		return false, "route_decider_rejected", nil
	}

	return true, "", nil
}
