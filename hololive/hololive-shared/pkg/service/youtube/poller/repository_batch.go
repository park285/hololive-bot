// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package poller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

const pollerBatchMaxSize = 50

type batchRepository interface {
	PersistVideos(ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error
	PersistCommunityPosts(ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error
}

type gormBatchRepository struct {
	db *gorm.DB
}

func newBatchRepository(db *gorm.DB) batchRepository {
	return &gormBatchRepository{db: db}
}

func (r *gormBatchRepository) PersistVideos(ctx context.Context, videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error {
	if err := validateShortNotificationPublishedAt(videos, notifications); err != nil {
		return fmt.Errorf("validate short notifications: %w", err)
	}

	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.batchUpsertVideos(ctx, tx, videos); err != nil {
			return fmt.Errorf("batch upsert videos: %w", err)
		}
		if err := r.resolveShortPersistedContentIDs(ctx, tx, notifications, trackingRows); err != nil {
			return fmt.Errorf("resolve short persisted content ids: %w", err)
		}
		sourcePosts := buildShortSourcePosts(videos, trackingRows)
		if err := trackingrepo.NewRepository(tx).UpsertSourcePostsBatch(ctx, sourcePosts); err != nil {
			return fmt.Errorf("upsert short source posts: %w", err)
		}
		if err := r.batchInsertNotifications(ctx, tx, notifications); err != nil {
			return fmt.Errorf("batch insert notifications: %w", err)
		}
		if err := reconcileTrackingRowsWithPersistedSendState(ctx, tx, trackingRows); err != nil {
			return fmt.Errorf("reconcile tracking rows with persisted send state: %w", err)
		}
		if err := trackingrepo.NewRepository(tx).UpsertBatch(ctx, trackingRows); err != nil {
			return fmt.Errorf("upsert video tracking: %w", err)
		}
		alarmStates := buildCommunityShortsAlarmStates(trackingRows)
		if err := trackingrepo.NewRepository(tx).UpsertAlarmStateBatch(ctx, alarmStates); err != nil {
			return fmt.Errorf("upsert short alarm states: %w", err)
		}
		if err := r.upsertWatermark(ctx, tx, watermark); err != nil {
			return fmt.Errorf("upsert watermark: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("persist videos transaction: %w", err)
	}
	r.persistLatencyClassificationsAfterCommit(ctx, trackingRows)
	return nil
}

func (r *gormBatchRepository) PersistCommunityPosts(ctx context.Context, posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking, watermark *domain.YouTubeContentWatermark) error {
	if err := validateCommunityNotificationPublishedAt(posts, notifications); err != nil {
		return fmt.Errorf("validate community notifications: %w", err)
	}

	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.batchUpsertCommunityPosts(ctx, tx, posts); err != nil {
			return fmt.Errorf("batch upsert community posts: %w", err)
		}
		sourcePosts := buildCommunitySourcePosts(posts, trackingRows)
		if err := trackingrepo.NewRepository(tx).UpsertSourcePostsBatch(ctx, sourcePosts); err != nil {
			return fmt.Errorf("upsert community source posts: %w", err)
		}
		if err := r.batchInsertNotifications(ctx, tx, notifications); err != nil {
			return fmt.Errorf("batch insert notifications: %w", err)
		}
		if err := reconcileTrackingRowsWithPersistedSendState(ctx, tx, trackingRows); err != nil {
			return fmt.Errorf("reconcile tracking rows with persisted send state: %w", err)
		}
		if err := trackingrepo.NewRepository(tx).UpsertBatch(ctx, trackingRows); err != nil {
			return fmt.Errorf("upsert community tracking: %w", err)
		}
		alarmStates := buildCommunityShortsAlarmStates(trackingRows)
		if err := trackingrepo.NewRepository(tx).UpsertAlarmStateBatch(ctx, alarmStates); err != nil {
			return fmt.Errorf("upsert community alarm states: %w", err)
		}
		if err := r.upsertWatermark(ctx, tx, watermark); err != nil {
			return fmt.Errorf("upsert watermark: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("persist community posts transaction: %w", err)
	}
	r.persistLatencyClassificationsAfterCommit(ctx, trackingRows)
	return nil
}

func (r *gormBatchRepository) persistLatencyClassificationsAfterCommit(
	ctx context.Context,
	trackingRows []*domain.YouTubeContentAlarmTracking,
) {
	if r == nil || r.db == nil || len(trackingRows) == 0 {
		return
	}

	identities := make([]outbox.PostTrackingIdentity, 0, len(trackingRows))
	for i := range trackingRows {
		if trackingRows[i] == nil {
			continue
		}
		identities = append(identities, outbox.PostTrackingIdentity{
			Kind:      trackingRows[i].Kind,
			ContentID: trackingRows[i].ContentID,
		})
	}
	if len(identities) == 0 {
		return
	}

	if err := outbox.NewDeliveryTelemetryRepository(r.db).PersistPostLatencyClassificationsByIdentities(ctx, identities); err != nil {
		slog.Default().Warn("Failed to persist post latency classifications after detection commit",
			slog.Int("tracking_rows", len(identities)),
			slog.Any("error", err),
		)
	}
}

type communityNotificationPublishedAtPayload struct {
	CanonicalPostID string     `json:"canonical_post_id"`
	PostID          string     `json:"post_id"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}

type shortNotificationPublishedAtPayload struct {
	CanonicalPostID string     `json:"canonical_post_id"`
	VideoID         string     `json:"video_id"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
}

func validateCanonicalNotificationIdentity(kind domain.OutboxKind, contentID, payloadID, canonicalPostID string) error {
	wantCanonicalContentID, err := ytcontentid.ForOutboxKind(kind, contentID)
	if err != nil {
		return fmt.Errorf("normalize content id: %w", err)
	}
	gotPayloadCanonicalID, err := ytcontentid.ForOutboxKind(kind, payloadID)
	if err != nil {
		return fmt.Errorf("normalize payload resource id: %w", err)
	}
	gotCanonicalPostID, err := ytcontentid.ForOutboxKind(kind, canonicalPostID)
	if err != nil {
		return fmt.Errorf("normalize canonical_post_id: %w", err)
	}

	if gotPayloadCanonicalID != wantCanonicalContentID {
		return fmt.Errorf("payload resource id mismatch: got %s want %s", gotPayloadCanonicalID, wantCanonicalContentID)
	}
	if gotCanonicalPostID != wantCanonicalContentID {
		return fmt.Errorf("payload canonical_post_id mismatch: got %s want %s", gotCanonicalPostID, wantCanonicalContentID)
	}

	return nil
}

func validateShortNotificationPublishedAt(videos []*domain.YouTubeVideo, notifications []*domain.YouTubeNotificationOutbox) error {
	if len(videos) == 0 || len(notifications) == 0 {
		return nil
	}

	videosByID := make(map[string]*domain.YouTubeVideo, len(videos)*2)
	for _, video := range videos {
		if video == nil || video.VideoID == "" {
			continue
		}
		videosByID[video.VideoID] = video
		videosByID[normalizeContentID(domain.OutboxKindNewShort, video.VideoID)] = video
	}
	if len(videosByID) == 0 {
		return nil
	}

	for _, notification := range notifications {
		if notification == nil || notification.Kind != domain.OutboxKindNewShort {
			continue
		}

		video, ok := videosByID[notification.ContentID]
		if !ok {
			continue
		}

		var payload shortNotificationPublishedAtPayload
		if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
			return fmt.Errorf("video %s: unmarshal payload: %w", video.VideoID, err)
		}
		if err := validateCanonicalNotificationIdentity(notification.Kind, notification.ContentID, payload.VideoID, payload.CanonicalPostID); err != nil {
			return fmt.Errorf("video %s: %w", video.VideoID, err)
		}

		if video.PublishedAt == nil {
			if payload.PublishedAt != nil {
				return fmt.Errorf("video %s: payload published_at set while video record is empty", video.VideoID)
			}
			continue
		}
		if payload.PublishedAt == nil {
			return fmt.Errorf("video %s: payload missing published_at", video.VideoID)
		}

		wantPublishedAt := yttimestamp.Format(*video.PublishedAt)
		gotPublishedAt := payload.PublishedAt.Format(yttimestamp.Canonical.Layout)
		if gotPublishedAt != wantPublishedAt {
			return fmt.Errorf("video %s: payload published_at mismatch: got %s want %s", video.VideoID, gotPublishedAt, wantPublishedAt)
		}
	}

	return nil
}

func (r *gormBatchRepository) resolveShortPersistedContentIDs(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox, trackingRows []*domain.YouTubeContentAlarmTracking) error {
	canonicalIDs := make([]string, 0, len(notifications)+len(trackingRows))
	aliasSet := make(map[string]struct{}, (len(notifications)+len(trackingRows))*2)

	collect := func(kind domain.OutboxKind, contentID string) {
		if kind != domain.OutboxKindNewShort {
			return
		}
		canonicalID := normalizeContentID(kind, contentID)
		if canonicalID == "" {
			return
		}
		if _, exists := aliasSet[canonicalID]; !exists {
			canonicalIDs = append(canonicalIDs, canonicalID)
		}
		aliasSet[canonicalID] = struct{}{}
		rawID := normalizeShortVideoResourceID(contentID)
		if rawID != "" {
			aliasSet[rawID] = struct{}{}
		}
	}

	for i := range notifications {
		if notifications[i] == nil {
			continue
		}
		collect(notifications[i].Kind, notifications[i].ContentID)
	}
	for i := range trackingRows {
		if trackingRows[i] == nil {
			continue
		}
		collect(trackingRows[i].Kind, trackingRows[i].ContentID)
	}
	if len(canonicalIDs) == 0 {
		return nil
	}

	aliases := make([]string, 0, len(aliasSet))
	for alias := range aliasSet {
		aliases = append(aliases, alias)
	}

	type shortIdentityRow struct {
		ContentID string `gorm:"column:content_id"`
	}

	resolvedByCanonical := make(map[string]string, len(canonicalIDs))
	recordResolved := func(contentID string) {
		canonicalID := normalizeContentID(domain.OutboxKindNewShort, contentID)
		if canonicalID == "" {
			return
		}
		if existing := resolvedByCanonical[canonicalID]; existing == canonicalID {
			return
		}
		if contentID == canonicalID {
			resolvedByCanonical[canonicalID] = canonicalID
			return
		}
		if _, exists := resolvedByCanonical[canonicalID]; !exists {
			resolvedByCanonical[canonicalID] = contentID
		}
	}

	var outboxRows []shortIdentityRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationOutbox{}).
		Select("content_id").
		Where("kind = ? AND content_id IN ?", domain.OutboxKindNewShort, aliases).
		Find(&outboxRows).Error; err != nil {
		return fmt.Errorf("load existing short outbox identities: %w", err)
	}
	for i := range outboxRows {
		recordResolved(strings.TrimSpace(outboxRows[i].ContentID))
	}

	var trackingIdentityRows []shortIdentityRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeContentAlarmTracking{}).
		Select("content_id").
		Where("kind = ? AND content_id IN ?", domain.OutboxKindNewShort, aliases).
		Find(&trackingIdentityRows).Error; err != nil {
		return fmt.Errorf("load existing short tracking identities: %w", err)
	}
	for i := range trackingIdentityRows {
		recordResolved(strings.TrimSpace(trackingIdentityRows[i].ContentID))
	}

	resolve := func(contentID string) string {
		canonicalID := normalizeContentID(domain.OutboxKindNewShort, contentID)
		if canonicalID == "" {
			return strings.TrimSpace(contentID)
		}
		if resolved := resolvedByCanonical[canonicalID]; resolved != "" {
			return resolved
		}
		return canonicalID
	}

	for i := range notifications {
		if notifications[i] == nil || notifications[i].Kind != domain.OutboxKindNewShort {
			continue
		}
		notifications[i].ContentID = resolve(notifications[i].ContentID)
	}
	for i := range trackingRows {
		if trackingRows[i] == nil || trackingRows[i].Kind != domain.OutboxKindNewShort {
			continue
		}
		trackingRows[i].ContentID = resolve(trackingRows[i].ContentID)
	}

	return nil
}

func validateCommunityNotificationPublishedAt(posts []*domain.YouTubeCommunityPost, notifications []*domain.YouTubeNotificationOutbox) error {
	if len(posts) == 0 || len(notifications) == 0 {
		return nil
	}

	postsByID := make(map[string]*domain.YouTubeCommunityPost, len(posts))
	for _, post := range posts {
		if post == nil || post.PostID == "" {
			continue
		}
		postsByID[post.PostID] = post
	}
	if len(postsByID) == 0 {
		return nil
	}

	for _, notification := range notifications {
		if notification == nil || notification.Kind != domain.OutboxKindCommunityPost {
			continue
		}

		post, ok := postsByID[notification.ContentID]
		if !ok {
			continue
		}

		var payload communityNotificationPublishedAtPayload
		if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
			return fmt.Errorf("post %s: unmarshal payload: %w", post.PostID, err)
		}
		if err := validateCanonicalNotificationIdentity(notification.Kind, notification.ContentID, payload.PostID, payload.CanonicalPostID); err != nil {
			return fmt.Errorf("post %s: %w", post.PostID, err)
		}

		if post.PublishedAt == nil {
			if payload.PublishedAt != nil {
				return fmt.Errorf("post %s: payload published_at set while post record is empty", post.PostID)
			}
			continue
		}
		if payload.PublishedAt == nil {
			return fmt.Errorf("post %s: payload missing published_at", post.PostID)
		}

		wantPublishedAt := yttimestamp.Format(*post.PublishedAt)
		gotPublishedAt := payload.PublishedAt.Format(yttimestamp.Canonical.Layout)
		if gotPublishedAt != wantPublishedAt {
			return fmt.Errorf("post %s: payload published_at mismatch: got %s want %s", post.PostID, gotPublishedAt, wantPublishedAt)
		}
	}

	return nil
}

type sourcePostKey struct {
	kind   domain.OutboxKind
	postID string
}

func buildShortSourcePosts(videos []*domain.YouTubeVideo, trackingRows []*domain.YouTubeContentAlarmTracking) []*domain.YouTubeCommunityShortsSourcePost {
	rowsByKey := make(map[sourcePostKey]*domain.YouTubeCommunityShortsSourcePost, len(videos)+len(trackingRows))
	fallbackDetectedAt := yttimestamp.Normalize(time.Now())

	for i := range trackingRows {
		if trackingRows[i] == nil || trackingRows[i].Kind != domain.OutboxKindNewShort {
			continue
		}
		postID := normalizeSourcePostID(domain.OutboxKindNewShort, trackingRows[i].ContentID)
		if postID == "" {
			continue
		}
		rowsByKey[sourcePostKey{kind: domain.OutboxKindNewShort, postID: postID}] = &domain.YouTubeCommunityShortsSourcePost{
			Kind:              domain.OutboxKindNewShort,
			PostID:            postID,
			ChannelID:         strings.TrimSpace(trackingRows[i].ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(trackingRows[i].ActualPublishedAt),
			DetectedAt:        yttimestamp.Normalize(trackingRows[i].DetectedAt),
		}
	}

	for i := range videos {
		if videos[i] == nil || !videos[i].IsShort {
			continue
		}
		postID := normalizeSourcePostID(domain.OutboxKindNewShort, videos[i].VideoID)
		if postID == "" {
			continue
		}
		key := sourcePostKey{kind: domain.OutboxKindNewShort, postID: postID}
		if row, ok := rowsByKey[key]; ok {
			if row.ActualPublishedAt == nil {
				row.ActualPublishedAt = yttimestamp.NormalizePtr(videos[i].PublishedAt)
			}
			if row.ChannelID == "" {
				row.ChannelID = strings.TrimSpace(videos[i].ChannelID)
			}
			continue
		}
		rowsByKey[key] = &domain.YouTubeCommunityShortsSourcePost{
			Kind:              domain.OutboxKindNewShort,
			PostID:            postID,
			ChannelID:         strings.TrimSpace(videos[i].ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(videos[i].PublishedAt),
			DetectedAt:        fallbackDetectedAt,
		}
	}

	return flattenSourcePosts(rowsByKey)
}

func buildCommunitySourcePosts(posts []*domain.YouTubeCommunityPost, trackingRows []*domain.YouTubeContentAlarmTracking) []*domain.YouTubeCommunityShortsSourcePost {
	rowsByKey := make(map[sourcePostKey]*domain.YouTubeCommunityShortsSourcePost, len(posts)+len(trackingRows))
	fallbackDetectedAt := yttimestamp.Normalize(time.Now())

	for i := range trackingRows {
		if trackingRows[i] == nil || trackingRows[i].Kind != domain.OutboxKindCommunityPost {
			continue
		}
		postID := normalizeSourcePostID(domain.OutboxKindCommunityPost, trackingRows[i].ContentID)
		if postID == "" {
			continue
		}
		rowsByKey[sourcePostKey{kind: domain.OutboxKindCommunityPost, postID: postID}] = &domain.YouTubeCommunityShortsSourcePost{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            postID,
			ChannelID:         strings.TrimSpace(trackingRows[i].ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(trackingRows[i].ActualPublishedAt),
			DetectedAt:        yttimestamp.Normalize(trackingRows[i].DetectedAt),
		}
	}

	for i := range posts {
		if posts[i] == nil {
			continue
		}
		postID := normalizeSourcePostID(domain.OutboxKindCommunityPost, posts[i].PostID)
		if postID == "" {
			continue
		}
		key := sourcePostKey{kind: domain.OutboxKindCommunityPost, postID: postID}
		if row, ok := rowsByKey[key]; ok {
			if row.ActualPublishedAt == nil {
				row.ActualPublishedAt = yttimestamp.NormalizePtr(posts[i].PublishedAt)
			}
			if row.ChannelID == "" {
				row.ChannelID = strings.TrimSpace(posts[i].ChannelID)
			}
			continue
		}
		rowsByKey[key] = &domain.YouTubeCommunityShortsSourcePost{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            postID,
			ChannelID:         strings.TrimSpace(posts[i].ChannelID),
			ActualPublishedAt: yttimestamp.NormalizePtr(posts[i].PublishedAt),
			DetectedAt:        fallbackDetectedAt,
		}
	}

	return flattenSourcePosts(rowsByKey)
}

func flattenSourcePosts(rowsByKey map[sourcePostKey]*domain.YouTubeCommunityShortsSourcePost) []*domain.YouTubeCommunityShortsSourcePost {
	rows := make([]*domain.YouTubeCommunityShortsSourcePost, 0, len(rowsByKey))
	for _, row := range rowsByKey {
		if row == nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func normalizeSourcePostID(kind domain.OutboxKind, postID string) string {
	normalizedPostID := strings.TrimSpace(postID)
	if normalizedPostID == "" {
		return ""
	}
	canonicalPostID, err := ytcontentid.ForOutboxKind(kind, normalizedPostID)
	if err == nil && strings.TrimSpace(canonicalPostID) != "" {
		return canonicalPostID
	}
	return normalizedPostID
}

func (r *gormBatchRepository) batchUpsertVideos(ctx context.Context, tx *gorm.DB, videos []*domain.YouTubeVideo) error {
	if len(videos) == 0 {
		return nil
	}

	for start := 0; start < len(videos); start += pollerBatchMaxSize {
		end := min(start+pollerBatchMaxSize, len(videos))
		chunk := videos[start:end]
		if err := r.upsertVideosChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *gormBatchRepository) upsertVideosChunk(ctx context.Context, tx *gorm.DB, videos []*domain.YouTubeVideo) error {
	now := time.Now()
	args := make([]any, 0, len(videos)*12)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_videos
			(video_id, channel_id, title, thumbnail, duration, published_text, published_at, is_short, is_live_replay, view_count, first_seen_at, last_seen_at)
		VALUES
	`)

	for i, video := range videos {
		publishedAt := yttimestamp.NormalizePtr(video.PublishedAt)
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			video.VideoID,
			video.ChannelID,
			video.Title,
			video.Thumbnail,
			video.Duration,
			video.PublishedText,
			publishedAt,
			video.IsShort,
			video.IsLiveReplay,
			video.ViewCount,
			now,
			now,
		)
	}

	sb.WriteString(`
		ON CONFLICT (video_id) DO UPDATE
		SET last_seen_at = EXCLUDED.last_seen_at,
		    published_at = COALESCE(EXCLUDED.published_at, youtube_videos.published_at),
		    view_count = EXCLUDED.view_count
	`)

	if err := tx.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("exec video upsert chunk (%d rows): %w", len(videos), err)
	}

	return nil
}

func (r *gormBatchRepository) batchUpsertCommunityPosts(ctx context.Context, tx *gorm.DB, posts []*domain.YouTubeCommunityPost) error {
	if len(posts) == 0 {
		return nil
	}

	for start := 0; start < len(posts); start += pollerBatchMaxSize {
		end := min(start+pollerBatchMaxSize, len(posts))
		chunk := posts[start:end]
		if err := r.upsertCommunityPostsChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *gormBatchRepository) upsertCommunityPostsChunk(ctx context.Context, tx *gorm.DB, posts []*domain.YouTubeCommunityPost) error {
	now := time.Now()
	args := make([]any, 0, len(posts)*13)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_community_posts
			(post_id, channel_id, author_name, author_photo, content_text, published_text, published_at, like_count, comment_count, images, attached_video, first_seen_at, last_seen_at)
		VALUES
	`)

	for i, post := range posts {
		publishedAt := yttimestamp.NormalizePtr(post.PublishedAt)
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			post.PostID,
			post.ChannelID,
			post.AuthorName,
			post.AuthorPhoto,
			post.ContentText,
			post.PublishedText,
			publishedAt,
			post.LikeCount,
			post.CommentCount,
			post.Images,
			post.AttachedVideo,
			now,
			now,
		)
	}

	sb.WriteString(`
		ON CONFLICT (post_id) DO UPDATE
		SET last_seen_at = EXCLUDED.last_seen_at,
		    published_at = COALESCE(EXCLUDED.published_at, youtube_community_posts.published_at),
		    like_count = EXCLUDED.like_count,
		    comment_count = EXCLUDED.comment_count
	`)

	if err := tx.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("exec community post upsert chunk (%d rows): %w", len(posts), err)
	}

	return nil
}

func (r *gormBatchRepository) batchInsertNotifications(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) error {
	if len(notifications) == 0 {
		return nil
	}

	for start := 0; start < len(notifications); start += pollerBatchMaxSize {
		end := min(start+pollerBatchMaxSize, len(notifications))
		chunk := notifications[start:end]
		if err := r.insertNotificationsChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *gormBatchRepository) insertNotificationsChunk(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) error {
	for i, notification := range notifications {
		if _, err := notification.DedupeKey(); err != nil {
			return fmt.Errorf("validate notification dedupe key at index %d: %w", i, err)
		}
	}

	completedSentAtByIdentity, err := loadCompletedNotificationSentAtByIdentity(ctx, tx, notifications)
	if err != nil {
		return fmt.Errorf("load completed notification sent state: %w", err)
	}
	failedRows, err := loadFailedNotificationOutboxRows(ctx, tx, notifications)
	if err != nil {
		return fmt.Errorf("load failed outbox rows: %w", err)
	}
	completedFailedRows, reactivationRows := partitionFailedNotificationOutboxRows(failedRows, completedSentAtByIdentity)
	if err := finalizeCompletedFailedNotificationRows(ctx, tx, completedFailedRows, completedSentAtByIdentity); err != nil {
		return fmt.Errorf("finalize completed failed notification rows: %w", err)
	}

	activeNotifications := filterCompletedNotifications(notifications, completedSentAtByIdentity)
	if len(activeNotifications) == 0 {
		return nil
	}

	now := time.Now()
	args := make([]any, 0, len(activeNotifications)*8)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_notification_outbox
			(kind, channel_id, content_id, payload, status, attempt_count, next_attempt_at, created_at)
		VALUES
	`)

	for i, notification := range activeNotifications {
		if i > 0 {
			sb.WriteByte(',')
		}

		status := notification.Status
		if status == "" {
			status = domain.OutboxStatusPending
		}
		nextAttemptAt := notification.NextAttemptAt
		if nextAttemptAt.IsZero() {
			nextAttemptAt = now
		}
		createdAt := notification.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}

		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(
			args,
			notification.Kind,
			notification.ChannelID,
			notification.ContentID,
			notification.Payload,
			status,
			notification.AttemptCount,
			nextAttemptAt,
			createdAt,
		)
	}

	sb.WriteString(`
		ON CONFLICT (kind, content_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    payload = EXCLUDED.payload,
		    status = 'PENDING',
		    attempt_count = 0,
		    next_attempt_at = EXCLUDED.next_attempt_at,
		    locked_at = NULL,
		    sent_at = NULL,
		    error = ''
		WHERE youtube_notification_outbox.status = 'FAILED'
		  AND youtube_notification_outbox.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	`)

	if err := tx.WithContext(ctx).Exec(sb.String(), args...).Error; err != nil {
		return fmt.Errorf("exec notification insert chunk (%d rows): %w", len(activeNotifications), err)
	}
	if err := rearmFailedDeliveryRows(ctx, tx, collectFailedNotificationOutboxIDs(reactivationRows), now); err != nil {
		return fmt.Errorf("rearm failed delivery rows: %w", err)
	}

	return nil
}

type failedNotificationOutboxRow struct {
	ID        int64             `gorm:"column:id"`
	Kind      domain.OutboxKind `gorm:"column:kind"`
	ContentID string            `gorm:"column:content_id"`
}

func loadFailedNotificationOutboxRows(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) ([]failedNotificationOutboxRow, error) {
	clauses := make([]string, 0, len(notifications))
	args := make([]any, 0, len(notifications)*2)
	seen := make(map[string]struct{}, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
			continue
		}
		contentID := strings.TrimSpace(notification.ContentID)
		if contentID == "" {
			continue
		}
		identityKey := notificationIdentityKey(notification.Kind, contentID)
		if _, ok := seen[identityKey]; ok {
			continue
		}
		seen[identityKey] = struct{}{}
		clauses = append(clauses, "(kind = ? AND content_id = ?)")
		args = append(args, notification.Kind, contentID)
	}
	if len(clauses) == 0 {
		return nil, nil
	}

	var rows []failedNotificationOutboxRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationOutbox{}).
		Select("id, kind, content_id").
		Where("status = ?", domain.OutboxStatusFailed).
		Where(strings.Join(clauses, " OR "), args...).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query failed outbox rows: %w", err)
	}
	return rows, nil
}

func loadCompletedNotificationSentAtByIdentity(ctx context.Context, tx *gorm.DB, notifications []*domain.YouTubeNotificationOutbox) (map[string]time.Time, error) {
	completed := make(map[string]time.Time)
	if len(notifications) == 0 || tx == nil {
		return completed, nil
	}

	repo := trackingrepo.NewRepository(tx)
	seen := make(map[string]struct{}, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
			continue
		}
		contentID := strings.TrimSpace(notification.ContentID)
		if contentID == "" {
			continue
		}
		identityKey := notificationIdentityKey(notification.Kind, contentID)
		if _, ok := seen[identityKey]; ok {
			continue
		}
		seen[identityKey] = struct{}{}

		trackingRow, err := repo.FindByIdentity(ctx, notification.Kind, contentID)
		if err != nil {
			return nil, fmt.Errorf("load notification tracking row: %w", err)
		}
		if trackingRow != nil && trackingRow.AlarmSentAt != nil && !trackingRow.AlarmSentAt.IsZero() {
			completed[identityKey] = yttimestamp.Normalize(*trackingRow.AlarmSentAt)
		}

		postID := resolveNotificationReactivationPostID(notification.Kind, contentID, notification.Payload)
		if postID == "" {
			continue
		}
		stateRow, err := repo.FindAlarmStateByPostID(ctx, notification.Kind, postID)
		if err != nil {
			return nil, fmt.Errorf("load notification alarm state: %w", err)
		}
		if stateRow != nil && stateRow.AlarmSentAt != nil && !stateRow.AlarmSentAt.IsZero() {
			completed[identityKey] = selectEarlierSentAt(completed[identityKey], yttimestamp.Normalize(*stateRow.AlarmSentAt))
		}
	}

	return completed, nil
}

func partitionFailedNotificationOutboxRows(rows []failedNotificationOutboxRow, completedSentAtByIdentity map[string]time.Time) ([]failedNotificationOutboxRow, []failedNotificationOutboxRow) {
	completed := make([]failedNotificationOutboxRow, 0, len(rows))
	reactivations := make([]failedNotificationOutboxRow, 0, len(rows))
	for i := range rows {
		identityKey := notificationIdentityKey(rows[i].Kind, rows[i].ContentID)
		if _, ok := completedSentAtByIdentity[identityKey]; ok {
			completed = append(completed, rows[i])
			continue
		}
		reactivations = append(reactivations, rows[i])
	}
	return completed, reactivations
}

func finalizeCompletedFailedNotificationRows(ctx context.Context, tx *gorm.DB, rows []failedNotificationOutboxRow, completedSentAtByIdentity map[string]time.Time) error {
	for i := range rows {
		identityKey := notificationIdentityKey(rows[i].Kind, rows[i].ContentID)
		sentAt, ok := completedSentAtByIdentity[identityKey]
		if !ok || sentAt.IsZero() {
			continue
		}
		sentAt = yttimestamp.Normalize(sentAt)

		if err := tx.WithContext(ctx).
			Model(&domain.YouTubeNotificationOutbox{}).
			Where("id = ? AND status = ?", rows[i].ID, domain.OutboxStatusFailed).
			Updates(map[string]any{
				"status":    domain.OutboxStatusSent,
				"locked_at": nil,
				"sent_at": gorm.Expr(
					"CASE WHEN sent_at IS NULL OR sent_at > ? THEN ? ELSE sent_at END",
					sentAt,
					sentAt,
				),
				"error": "",
			}).Error; err != nil {
			return fmt.Errorf("update completed outbox row %d: %w", rows[i].ID, err)
		}

		if err := tx.WithContext(ctx).
			Model(&domain.YouTubeNotificationDelivery{}).
			Where("outbox_id = ? AND status = ?", rows[i].ID, domain.OutboxStatusFailed).
			Updates(map[string]any{
				"status":    domain.OutboxStatusSent,
				"locked_at": nil,
				"sent_at": gorm.Expr(
					"CASE WHEN sent_at IS NULL OR sent_at > ? THEN ? ELSE sent_at END",
					sentAt,
					sentAt,
				),
				"error": "",
			}).Error; err != nil {
			return fmt.Errorf("update completed delivery rows for outbox %d: %w", rows[i].ID, err)
		}
	}

	return nil
}

func filterCompletedNotifications(notifications []*domain.YouTubeNotificationOutbox, completedSentAtByIdentity map[string]time.Time) []*domain.YouTubeNotificationOutbox {
	if len(notifications) == 0 {
		return nil
	}

	filtered := make([]*domain.YouTubeNotificationOutbox, 0, len(notifications))
	for i := range notifications {
		notification := notifications[i]
		if notification == nil || !isCommunityShortsOutboxKind(notification.Kind) {
			filtered = append(filtered, notification)
			continue
		}
		if _, ok := completedSentAtByIdentity[notificationIdentityKey(notification.Kind, notification.ContentID)]; ok {
			continue
		}
		filtered = append(filtered, notification)
	}
	return filtered
}

func collectFailedNotificationOutboxIDs(rows []failedNotificationOutboxRow) []int64 {
	if len(rows) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for i := range rows {
		if _, ok := seen[rows[i].ID]; ok {
			continue
		}
		seen[rows[i].ID] = struct{}{}
		ids = append(ids, rows[i].ID)
	}
	return ids
}

func resolveNotificationReactivationPostID(kind domain.OutboxKind, contentID, payload string) string {
	switch kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort:
		var parsed shortNotificationPublishedAtPayload
		if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
			if postID := strings.TrimSpace(parsed.CanonicalPostID); postID != "" {
				return postID
			}
			if postID := strings.TrimSpace(contentID); postID != "" {
				return postID
			}
			if postID := strings.TrimSpace(parsed.VideoID); postID != "" {
				return postID
			}
		}
	case domain.OutboxKindCommunityPost:
		var parsed communityNotificationPublishedAtPayload
		if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
			if postID := strings.TrimSpace(parsed.CanonicalPostID); postID != "" {
				return postID
			}
			if postID := strings.TrimSpace(contentID); postID != "" {
				return postID
			}
			if postID := strings.TrimSpace(parsed.PostID); postID != "" {
				return postID
			}
		}
	}

	return strings.TrimSpace(contentID)
}

func notificationIdentityKey(kind domain.OutboxKind, contentID string) string {
	return fmt.Sprintf("%s::%s", kind, strings.TrimSpace(contentID))
}

func selectEarlierSentAt(current time.Time, candidate time.Time) time.Time {
	if current.IsZero() {
		return candidate
	}
	if candidate.IsZero() {
		return current
	}
	if candidate.Before(current) {
		return candidate
	}
	return current
}

func rearmFailedDeliveryRows(ctx context.Context, tx *gorm.DB, outboxIDs []int64, nextAttemptAt time.Time) error {
	if len(outboxIDs) == 0 {
		return nil
	}

	result := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationDelivery{}).
		Where("outbox_id IN ? AND status = ?", outboxIDs, domain.OutboxStatusFailed).
		Updates(map[string]any{
			"status":          domain.OutboxStatusPending,
			"attempt_count":   0,
			"next_attempt_at": nextAttemptAt,
			"locked_at":       nil,
			"sent_at":         nil,
			"error":           "",
		})
	if result.Error != nil {
		return fmt.Errorf("update delivery rows: %w", result.Error)
	}
	return nil
}

type persistedOutboxSentStateRow struct {
	ID        int64             `gorm:"column:id"`
	Kind      domain.OutboxKind `gorm:"column:kind"`
	ContentID string            `gorm:"column:content_id"`
	SentAt    *time.Time        `gorm:"column:sent_at"`
}

type persistedDeliverySentStateRow struct {
	OutboxID int64      `gorm:"column:outbox_id"`
	SentAt   *time.Time `gorm:"column:sent_at"`
}

func reconcileTrackingRowsWithPersistedSendState(ctx context.Context, tx *gorm.DB, trackingRows []*domain.YouTubeContentAlarmTracking) error {
	if len(trackingRows) == 0 || tx == nil {
		return nil
	}

	clauses := make([]string, 0, len(trackingRows))
	args := make([]any, 0, len(trackingRows)*2)
	identitySeen := make(map[string]struct{}, len(trackingRows))
	for i := range trackingRows {
		row := trackingRows[i]
		if row == nil || !isCommunityShortsOutboxKind(row.Kind) {
			continue
		}
		contentID := strings.TrimSpace(row.ContentID)
		if contentID == "" {
			continue
		}
		identityKey := fmt.Sprintf("%s::%s", row.Kind, contentID)
		if _, ok := identitySeen[identityKey]; ok {
			continue
		}
		identitySeen[identityKey] = struct{}{}
		clauses = append(clauses, "(kind = ? AND content_id = ?)")
		args = append(args, row.Kind, contentID)
	}
	if len(clauses) == 0 {
		return nil
	}

	var outboxRows []persistedOutboxSentStateRow
	if err := tx.WithContext(ctx).
		Model(&domain.YouTubeNotificationOutbox{}).
		Select("id, kind, content_id, sent_at").
		Where(strings.Join(clauses, " OR "), args...).
		Find(&outboxRows).Error; err != nil {
		return fmt.Errorf("query outbox rows: %w", err)
	}
	if len(outboxRows) == 0 {
		return nil
	}

	sentAtByIdentity := make(map[string]time.Time, len(outboxRows))
	outboxIDByIdentity := make(map[string]int64, len(outboxRows))
	outboxIDs := make([]int64, 0, len(outboxRows))
	for i := range outboxRows {
		contentID := strings.TrimSpace(outboxRows[i].ContentID)
		if contentID == "" {
			continue
		}
		identityKey := fmt.Sprintf("%s::%s", outboxRows[i].Kind, contentID)
		outboxIDByIdentity[identityKey] = outboxRows[i].ID
		outboxIDs = append(outboxIDs, outboxRows[i].ID)
		if outboxRows[i].SentAt != nil && !outboxRows[i].SentAt.IsZero() {
			updateIdentitySentAtMin(sentAtByIdentity, identityKey, yttimestamp.Normalize(*outboxRows[i].SentAt))
		}
	}

	if len(outboxIDs) > 0 {
		var deliveryRows []persistedDeliverySentStateRow
		if err := tx.WithContext(ctx).
			Model(&domain.YouTubeNotificationDelivery{}).
			Select("outbox_id, sent_at").
			Where("outbox_id IN ? AND status = ? AND sent_at IS NOT NULL", outboxIDs, domain.OutboxStatusSent).
			Scan(&deliveryRows).Error; err != nil {
			return fmt.Errorf("query sent delivery rows: %w", err)
		}

		identityByOutboxID := make(map[int64]string, len(outboxIDByIdentity))
		for identityKey, outboxID := range outboxIDByIdentity {
			identityByOutboxID[outboxID] = identityKey
		}
		for i := range deliveryRows {
			identityKey, ok := identityByOutboxID[deliveryRows[i].OutboxID]
			if !ok || deliveryRows[i].SentAt == nil || deliveryRows[i].SentAt.IsZero() {
				continue
			}
			updateIdentitySentAtMin(sentAtByIdentity, identityKey, yttimestamp.Normalize(*deliveryRows[i].SentAt))
		}
	}

	if len(sentAtByIdentity) == 0 {
		return nil
	}
	for i := range trackingRows {
		row := trackingRows[i]
		if row == nil || !isCommunityShortsOutboxKind(row.Kind) {
			continue
		}
		contentID := strings.TrimSpace(row.ContentID)
		if contentID == "" {
			continue
		}
		identityKey := fmt.Sprintf("%s::%s", row.Kind, contentID)
		sentAt, ok := sentAtByIdentity[identityKey]
		if !ok {
			continue
		}
		switch {
		case row.AlarmSentAt == nil:
			sentAtCopy := sentAt
			row.AlarmSentAt = &sentAtCopy
		case sentAt.Before(*row.AlarmSentAt):
			sentAtCopy := sentAt
			row.AlarmSentAt = &sentAtCopy
		}
	}

	return nil
}

func updateIdentitySentAtMin(sentAtByIdentity map[string]time.Time, identityKey string, candidate time.Time) {
	if candidate.IsZero() {
		return
	}
	if existing, ok := sentAtByIdentity[identityKey]; ok {
		if candidate.Before(existing) {
			sentAtByIdentity[identityKey] = candidate
		}
		return
	}
	sentAtByIdentity[identityKey] = candidate
}

func isCommunityShortsOutboxKind(kind domain.OutboxKind) bool {
	switch kind {
	case domain.OutboxKindCommunityPost, domain.OutboxKindNewShort:
		return true
	default:
		return false
	}
}

func (r *gormBatchRepository) upsertWatermark(ctx context.Context, tx *gorm.DB, watermark *domain.YouTubeContentWatermark) error {
	if watermark == nil {
		return nil
	}

	now := time.Now()
	if err := tx.WithContext(ctx).Exec(`
		INSERT INTO youtube_content_watermarks
			(channel_id, watermark_type, initialized, last_content_id, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (channel_id, watermark_type) DO UPDATE
		SET initialized = EXCLUDED.initialized,
		    last_content_id = EXCLUDED.last_content_id,
		    updated_at = EXCLUDED.updated_at
	`,
		watermark.ChannelID,
		watermark.WatermarkType,
		watermark.Initialized,
		watermark.LastContentID,
		now,
	).Error; err != nil {
		return fmt.Errorf("exec watermark upsert: %w", err)
	}

	return nil
}
