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

package pollers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling/batchrepo"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type CommunityPoller struct {
	client                           *scraper.Client
	db                               *gorm.DB
	repository                       batchrepo.BatchRepository
	maxResults                       int
	keywords                         []string
	routeDecider                     polling.NotificationRouteDecider
	inlinePublishedAtFallbackEnabled bool
	metrics                          *polling.Metrics
}

func NewCommunityPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int, keywords []string, routeDecider polling.NotificationRouteDecider, inlinePublishedAtFallbackEnabled ...bool) *CommunityPoller {
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
		repository:                       batchrepo.NewBatchRepository(db),
		maxResults:                       maxResults,
		keywords:                         keywords,
		routeDecider:                     routeDecider,
		inlinePublishedAtFallbackEnabled: inlineFallbackEnabled,
	}
}

func (p *CommunityPoller) SetMetrics(m *polling.Metrics) {
	if p == nil {
		return
	}
	p.metrics = m
}

func (p *CommunityPoller) ensureMetrics() *polling.Metrics {
	if p.metrics != nil {
		return p.metrics
	}
	return polling.NewMetrics()
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

	posts = polling.NormalizeCollectedCommunityPostsByCanonicalPostID(posts)
	if len(posts) == 0 {
		return nil
	}

	watermark, isInitialized, err := loadContentWatermark(ctx, p.db, channelID, domain.WatermarkTypeCommunityPost)
	if err != nil {
		return err
	}
	newPosts := collectNewCommunityPosts(posts, watermark, isInitialized)
	detectedAt := yttimestamp.Normalize(time.Now())
	observeCommunityShortsDetectionBatch(ctx, channelID, domain.AlarmTypeCommunity, len(newPosts), detectedAt, p.ensureMetrics())
	batch := p.buildCommunityBatch(ctx, channelID, newPosts, isInitialized, detectedAt)

	if err := p.repository.PersistCommunityPosts(ctx, batch.dbPosts, batch.notifications, batch.trackingRows, &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: selectLastWatermarkContentID(domain.OutboxKindCommunityPost, posts[0].PostID, watermark.LastContentID, batch.keepExistingWatermark),
	}); err != nil {
		return fmt.Errorf("persist community batch: %w", err)
	}

	return nil
}

type communityPollBatch struct {
	dbPosts               []*domain.YouTubeCommunityPost
	notifications         []*domain.YouTubeNotificationOutbox
	trackingRows          []*domain.YouTubeContentAlarmTracking
	keepExistingWatermark bool
}

func (p *CommunityPoller) buildCommunityBatch(
	ctx context.Context,
	channelID string,
	posts []*scraper.CommunityPost,
	isInitialized bool,
	detectedAt time.Time,
) communityPollBatch {
	batch := communityPollBatch{
		dbPosts:       make([]*domain.YouTubeCommunityPost, 0, len(posts)),
		notifications: make([]*domain.YouTubeNotificationOutbox, 0, len(posts)),
		trackingRows:  make([]*domain.YouTubeContentAlarmTracking, 0, len(posts)),
	}
	for i := range posts {
		dbPost, trackingRow, notification, keepExistingWatermark := p.buildCommunityPostArtifacts(ctx, channelID, posts[i], isInitialized, detectedAt)
		if dbPost != nil {
			batch.dbPosts = append(batch.dbPosts, dbPost)
		}
		if trackingRow != nil {
			batch.trackingRows = append(batch.trackingRows, trackingRow)
		}
		if notification != nil {
			batch.notifications = append(batch.notifications, notification)
		}
		batch.keepExistingWatermark = batch.keepExistingWatermark || keepExistingWatermark
	}
	return batch
}

func (p *CommunityPoller) buildCommunityPostArtifacts(
	ctx context.Context,
	channelID string,
	post *scraper.CommunityPost,
	isInitialized bool,
	detectedAt time.Time,
) (*domain.YouTubeCommunityPost, *domain.YouTubeContentAlarmTracking, *domain.YouTubeNotificationOutbox, bool) {
	if post == nil {
		return nil, nil, nil, false
	}

	canonicalPostID := polling.NormalizeContentID(domain.OutboxKindCommunityPost, post.PostID)
	publishedAt := yttimestamp.NormalizePtr(post.PublishedAt)
	if isInitialized && publishedAt == nil && p.inlinePublishedAtFallbackEnabled {
		publishedAt = p.resolveCommunityPublishedAtInline(ctx, polling.NormalizeCommunityResourceID(post.PostID))
	}
	logCommunityPostDetected(ctx, channelID, canonicalPostID, publishedAt, detectedAt)

	dbPost := &domain.YouTubeCommunityPost{
		PostID:        canonicalPostID,
		ChannelID:     channelID,
		AuthorName:    post.AuthorName,
		AuthorPhoto:   polling.ConvertThumbnails(post.AuthorPhoto),
		ContentText:   post.ContentText,
		PublishedText: post.PublishedText,
		PublishedAt:   publishedAt,
		LikeCount:     post.LikeCount,
		CommentCount:  post.CommentCount,
		Images:        polling.ConvertThumbnails(post.Images),
		AttachedVideo: post.VideoID,
	}
	if !isInitialized || !p.matchesKeywords(post.ContentText) {
		return dbPost, nil, nil, false
	}

	trackingRow := &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         canonicalPostID,
		ChannelID:         channelID,
		ActualPublishedAt: dbPost.PublishedAt,
		DetectedAt:        detectedAt,
	}
	notification, keepExistingWatermark := p.buildCommunityNotification(channelID, canonicalPostID, dbPost)
	return dbPost, trackingRow, notification, keepExistingWatermark
}

func (p *CommunityPoller) buildCommunityNotification(
	channelID string,
	canonicalPostID string,
	dbPost *domain.YouTubeCommunityPost,
) (*domain.YouTubeNotificationOutbox, bool) {
	routePublishedAt := derefTime(dbPost.PublishedAt)
	if p.routeDecider != nil && routePublishedAt.IsZero() {
		return nil, p.inlinePublishedAtFallbackEnabled
	}
	if p.routeDecider != nil && !polling.ShouldEnqueueRoutedNotification(p.routeDecider, domain.AlarmTypeCommunity, channelID, routePublishedAt) {
		return nil, false
	}

	return &domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: channelID,
		ContentID: canonicalPostID,
		Payload:   polling.BuildCommunityNotificationPayload(dbPost, canonicalPostID),
		Status:    domain.OutboxStatusPending,
	}, false
}

func collectNewCommunityPosts(
	posts []*scraper.CommunityPost,
	watermark domain.YouTubeContentWatermark,
	isInitialized bool,
) []*scraper.CommunityPost {
	newPosts := make([]*scraper.CommunityPost, 0, len(posts))
	for _, post := range posts {
		canonicalPostID := polling.NormalizeContentID(domain.OutboxKindCommunityPost, post.PostID)
		if isInitialized && canonicalPostID == polling.NormalizeContentID(domain.OutboxKindCommunityPost, watermark.LastContentID) {
			break
		}
		newPosts = append(newPosts, post)
	}
	return newPosts
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

func loadContentWatermark(
	ctx context.Context,
	db *gorm.DB,
	channelID string,
	watermarkType domain.WatermarkType,
) (domain.YouTubeContentWatermark, bool, error) {
	var watermark domain.YouTubeContentWatermark
	err := db.WithContext(ctx).Where(
		"channel_id = ? AND watermark_type = ?",
		channelID, watermarkType,
	).First(&watermark).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.YouTubeContentWatermark{}, false, fmt.Errorf("load %s watermark: %w", watermarkType, err)
	}
	return watermark, err == nil && watermark.Initialized, nil
}

func selectLastWatermarkContentID(kind domain.OutboxKind, latestID, existingID string, keepExisting bool) string {
	if keepExisting && strings.TrimSpace(existingID) != "" {
		return existingID
	}
	return polling.NormalizeContentID(kind, latestID)
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
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
