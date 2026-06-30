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
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/batchrepo"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type CommunityPoller struct {
	client     *scraper.Client
	db         pollerDB
	repository batchrepo.BatchRepository
	maxResults int
	keywords   []string
	metrics    *polling.Metrics
}

func NewCommunityPoller(scraperClient *scraper.Client, db any, maxResults int, keywords []string) *CommunityPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	querier := normalizePollerDB(db)
	return &CommunityPoller{
		client:     scraperClient,
		db:         querier,
		repository: batchrepo.NewPgxBatchRepositoryWithPersister(querier, newDeliveryTelemetryLatencyPersisterAdapter(querier)),
		maxResults: maxResults,
		keywords:   normalizeCommunityKeywords(keywords),
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
	newPosts := collectNewCommunityPosts(posts, &watermark, isInitialized)
	detectedAt := yttimestamp.Normalize(time.Now()).Truncate(time.Microsecond)
	observeCommunityShortsDetectionBatch(ctx, channelID, domain.AlarmTypeCommunity, len(newPosts), detectedAt, p.ensureMetrics())
	batch := p.buildCommunityBatch(ctx, channelID, newPosts, isInitialized, detectedAt)

	if err := p.repository.PersistCommunityPosts(ctx, batch.dbPosts, batch.notifications, batch.trackingRows, &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: polling.NormalizeContentID(domain.OutboxKindCommunityPost, posts[0].PostID),
	}); err != nil {
		return fmt.Errorf("persist community batch: %w", err)
	}

	return nil
}

type communityPollBatch struct {
	dbPosts       []*domain.YouTubeCommunityPost
	notifications []*domain.YouTubeNotificationOutbox
	trackingRows  []*domain.YouTubeContentAlarmTracking
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
		dbPost, trackingRow, notification := p.buildCommunityPostArtifacts(ctx, channelID, posts[i], isInitialized, detectedAt)
		if dbPost != nil {
			batch.dbPosts = append(batch.dbPosts, dbPost)
		}
		if trackingRow != nil {
			batch.trackingRows = append(batch.trackingRows, trackingRow)
		}
		if notification != nil {
			batch.notifications = append(batch.notifications, notification)
		}
	}
	return batch
}

func (p *CommunityPoller) buildCommunityPostArtifacts(
	ctx context.Context,
	channelID string,
	post *scraper.CommunityPost,
	isInitialized bool,
	detectedAt time.Time,
) (*domain.YouTubeCommunityPost, *domain.YouTubeContentAlarmTracking, *domain.YouTubeNotificationOutbox) {
	if post == nil {
		return nil, nil, nil
	}

	canonicalPostID := polling.NormalizeContentID(domain.OutboxKindCommunityPost, post.PostID)
	publishedAt := yttimestamp.NormalizePtr(post.PublishedAt)
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
		return dbPost, nil, nil
	}

	trackingRow := &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         canonicalPostID,
		ChannelID:         channelID,
		ActualPublishedAt: dbPost.PublishedAt,
		DetectedAt:        detectedAt,
	}
	notification := p.buildCommunityNotification(channelID, canonicalPostID, dbPost)
	return dbPost, trackingRow, notification
}

func (p *CommunityPoller) buildCommunityNotification(
	channelID string,
	canonicalPostID string,
	dbPost *domain.YouTubeCommunityPost,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: channelID,
		ContentID: canonicalPostID,
		Payload:   polling.BuildCommunityNotificationPayload(dbPost, canonicalPostID),
		Status:    domain.OutboxStatusPending,
	}
}

func collectNewCommunityPosts(
	posts []*scraper.CommunityPost,
	watermark *domain.YouTubeContentWatermark,
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
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}
	return false
}

func normalizeCommunityKeywords(keywords []string) []string {
	if len(keywords) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))
	for i := range keywords {
		keyword := strings.ToLower(strings.TrimSpace(keywords[i]))
		if keyword == "" {
			continue
		}
		if _, ok := seen[keyword]; ok {
			continue
		}
		seen[keyword] = struct{}{}
		normalized = append(normalized, keyword)
	}
	return normalized
}
