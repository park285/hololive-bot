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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type CommunityPoller struct {
	client                           *scraper.Client
	db                               *gorm.DB
	repo                             batchRepository
	maxResults                       int
	keywords                         []string
	routeDecider                     NotificationRouteDecider
	inlinePublishedAtFallbackEnabled bool
}

func NewCommunityPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int, keywords []string, routeDecider NotificationRouteDecider, inlinePublishedAtFallbackEnabled ...bool) *CommunityPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	inlineFallbackEnabled := false
	if len(inlinePublishedAtFallbackEnabled) > 0 {
		inlineFallbackEnabled = inlinePublishedAtFallbackEnabled[0]
	}
	return &CommunityPoller{
		client:                           scraperClient,
		db:                               db,
		repo:                             newBatchRepository(db),
		maxResults:                       maxResults,
		keywords:                         keywords,
		routeDecider:                     routeDecider,
		inlinePublishedAtFallbackEnabled: inlineFallbackEnabled,
	}
}

func (p *CommunityPoller) Name() string {
	return "community"
}

func (p *CommunityPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *CommunityPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *CommunityPoller) Poll(ctx context.Context, channelID string) error {
	posts, err := p.client.GetCommunityPosts(ctx, channelID, p.maxResults)
	if err != nil {
		return fmt.Errorf("failed to get community posts: %w", err)
	}

	posts = normalizeCollectedCommunityPostsByCanonicalPostID(posts)
	if len(posts) == 0 {
		return nil
	}

	var watermark domain.YouTubeContentWatermark
	err = p.db.WithContext(ctx).Where(
		"channel_id = ? AND watermark_type = ?",
		channelID, domain.WatermarkTypeCommunityPost,
	).First(&watermark).Error

	isInitialized := err == nil && watermark.Initialized
	newPosts := make([]*scraper.CommunityPost, 0, len(posts))
	for _, post := range posts {
		canonicalPostID := normalizeContentID(domain.OutboxKindCommunityPost, post.PostID)
		if isInitialized && canonicalPostID == normalizeContentID(domain.OutboxKindCommunityPost, watermark.LastContentID) {
			break
		}
		newPosts = append(newPosts, post)
	}

	dbPosts := make([]*domain.YouTubeCommunityPost, 0, len(newPosts))
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, len(newPosts))
	trackingRows := make([]*domain.YouTubeContentAlarmTracking, 0, len(newPosts))
	detectedAt := yttimestamp.Normalize(time.Now())
	keepExistingWatermark := false
	observeCommunityShortsDetectionBatch(ctx, channelID, domain.AlarmTypeCommunity, len(newPosts), detectedAt)
	for _, post := range newPosts {
		canonicalPostID := normalizeContentID(domain.OutboxKindCommunityPost, post.PostID)
		resourcePostID := normalizeCommunityResourceID(post.PostID)
		authorPhoto := convertThumbnails(post.AuthorPhoto)
		images := convertThumbnails(post.Images)
		matchesKeywords := p.matchesKeywords(post.ContentText)
		publishedAt := yttimestamp.NormalizePtr(post.PublishedAt)
		if isInitialized && publishedAt == nil && p.inlinePublishedAtFallbackEnabled {
			publishedAt = p.resolveCommunityPublishedAtInline(ctx, resourcePostID)
		}
		logCommunityPostDetected(ctx, channelID, canonicalPostID, publishedAt, detectedAt)

		dbPost := &domain.YouTubeCommunityPost{
			PostID:        canonicalPostID,
			ChannelID:     channelID,
			AuthorName:    post.AuthorName,
			AuthorPhoto:   authorPhoto,
			ContentText:   post.ContentText,
			PublishedText: post.PublishedText,
			PublishedAt:   publishedAt,
			LikeCount:     post.LikeCount,
			CommentCount:  post.CommentCount,
			Images:        images,
			AttachedVideo: post.VideoID,
		}

		dbPosts = append(dbPosts, dbPost)

		if isInitialized && matchesKeywords {
			trackingRows = append(trackingRows, &domain.YouTubeContentAlarmTracking{
				Kind:              domain.OutboxKindCommunityPost,
				ContentID:         canonicalPostID,
				ChannelID:         channelID,
				ActualPublishedAt: dbPost.PublishedAt,
				DetectedAt:        detectedAt,
			})

			var routePublishedAt time.Time
			if dbPost.PublishedAt != nil {
				routePublishedAt = *dbPost.PublishedAt
			}
			if p.routeDecider != nil && routePublishedAt.IsZero() {
				if p.inlinePublishedAtFallbackEnabled {
					keepExistingWatermark = true
				}
				continue
			}
			if p.routeDecider == nil || shouldEnqueueRoutedNotification(p.routeDecider, domain.AlarmTypeCommunity, channelID, routePublishedAt) {
				notifications = append(notifications, &domain.YouTubeNotificationOutbox{
					Kind:      domain.OutboxKindCommunityPost,
					ChannelID: channelID,
					ContentID: canonicalPostID,
					Payload:   buildCommunityNotificationPayload(dbPost, canonicalPostID),
					Status:    domain.OutboxStatusPending,
				})
			}
		}
	}
	lastContentID := normalizeContentID(domain.OutboxKindCommunityPost, posts[0].PostID)
	if keepExistingWatermark && strings.TrimSpace(watermark.LastContentID) != "" {
		lastContentID = watermark.LastContentID
	}

	if err := p.repo.PersistCommunityPosts(ctx, dbPosts, notifications, trackingRows, &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: lastContentID,
	}); err != nil {
		return fmt.Errorf("persist community batch: %w", err)
	}

	return nil
}

func (p *CommunityPoller) resolveCommunityPublishedAtInline(ctx context.Context, postID string) *time.Time {
	if strings.TrimSpace(postID) == "" {
		return nil
	}

	resolveCtx, cancel := context.WithTimeout(ctx, inlinePublishedAtFallbackTimeout)
	defer cancel()

	publishedAt, err := p.client.ResolveCommunityPostPublishedAt(resolveCtx, postID)
	if err != nil {
		if errors.Is(err, scraper.ErrCommunityPublishedAtNotFound) {
			return nil
		}
		slog.WarnContext(ctx, "community published_at inline fallback failed",
			"post_id", postID,
			"error", err,
		)
		return nil
	}

	return yttimestamp.NormalizePtr(publishedAt)
}

func logCommunityPostDetected(ctx context.Context, channelID, postID string, actualPublishedAt *time.Time, detectedAt time.Time) {
	slog.LogAttrs(ctx, slog.LevelInfo, communityPostDetectedLogMessage,
		slog.String(logschema.FieldChannelID, channelID),
		slog.String(logschema.FieldPostID, postID),
		optionalTimestampAttr(logschema.FieldActualPublishedAt, actualPublishedAt),
		slog.String(logschema.FieldDetectedAt, yttimestamp.Format(detectedAt)),
	)
}

func optionalTimestampAttr(key string, value *time.Time) slog.Attr {
	if value == nil {
		return slog.Any(key, nil)
	}
	return slog.String(key, yttimestamp.Format(*value))
}

// matchesKeywords: 텍스트가 키워드 조건에 맞는지 확인
// 키워드가 비어있으면 항상 true (모든 포스트 매칭)
func (p *CommunityPoller) matchesKeywords(text string) bool {
	if len(p.keywords) == 0 {
		return true
	}

	lowerText := strings.ToLower(text)
	for _, keyword := range p.keywords {
		if strings.Contains(lowerText, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}
