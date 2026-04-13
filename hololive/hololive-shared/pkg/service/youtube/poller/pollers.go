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
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

const (
	communityPostDetectedLogMessage  = logschema.CommunityPostDetectedMessage
	shortDetectedLogMessage          = logschema.ShortDetectedMessage
	inlinePublishedAtFallbackTimeout = 10 * time.Second
)

type ChannelStatsPoller struct {
	client          *scraper.Client
	db              *gorm.DB
	profileCacheTTL time.Duration
}

func NewChannelStatsPoller(scraperClient *scraper.Client, db *gorm.DB) *ChannelStatsPoller {
	return &ChannelStatsPoller{
		client:          scraperClient,
		db:              db,
		profileCacheTTL: 24 * time.Hour,
	}
}

func (p *ChannelStatsPoller) Name() string {
	return "channel_stats"
}

func (p *ChannelStatsPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *ChannelStatsPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *ChannelStatsPoller) Poll(ctx context.Context, channelID string) error {
	stats, err := p.client.GetChannelStats(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel stats: %w", err)
	}

	snapshot := &domain.YouTubeChannelStatsSnapshot{
		ChannelID:       channelID,
		CapturedAt:      time.Now(),
		SubscriberCount: stats.SubscriberCount,
		ViewCount:       stats.ViewCount,
		VideoCount:      stats.VideoCount,
		JoinedDate:      stats.JoinedDate,
		Description:     stats.Description,
		Country:         stats.Country,
		Handle:          stats.Handle,
	}

	if err := p.db.WithContext(ctx).Create(snapshot).Error; err != nil {
		return fmt.Errorf("failed to save channel stats snapshot: %w", err)
	}

	slog.Debug("Channel stats snapshot saved",
		"channel_id", channelID,
		"subscriber_count", stats.SubscriberCount)

	p.updateProfileIfStale(ctx, channelID)

	return nil
}

func (p *ChannelStatsPoller) updateProfileIfStale(ctx context.Context, channelID string) {
	var profile domain.YouTubeChannelProfile
	err := p.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&profile).Error

	needsUpdate := err != nil || time.Since(profile.UpdatedAt) > p.profileCacheTTL

	if !needsUpdate {
		return
	}

	snippet, err := p.client.GetChannelSnippet(ctx, channelID)
	if err != nil {
		slog.Warn("Failed to get channel snippet for profile update",
			"channel_id", channelID,
			"error", err)
		return
	}

	avatars := convertThumbnails(snippet.Avatar)
	banners := convertThumbnails(snippet.Banner)

	newProfile := &domain.YouTubeChannelProfile{
		ChannelID: channelID,
		Avatar:    avatars,
		Banner:    banners,
	}

	p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"avatar", "banner", "updated_at"}),
	}).Create(newProfile)

	slog.Debug("Channel profile updated",
		"channel_id", channelID,
		"avatar_count", len(avatars),
		"banner_count", len(banners))
}

type VideosPoller struct {
	client     *scraper.Client
	db         *gorm.DB
	repo       batchRepository
	maxResults int
}

func NewVideosPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int) *VideosPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	return &VideosPoller{
		client:     scraperClient,
		db:         db,
		repo:       newBatchRepository(db),
		maxResults: maxResults,
	}
}

func (p *VideosPoller) Name() string {
	return "videos"
}

func (p *VideosPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *VideosPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *VideosPoller) Poll(ctx context.Context, channelID string) error {
	videos, err := p.client.GetRecentVideos(ctx, channelID, p.maxResults)
	if err != nil {
		return fmt.Errorf("failed to get recent videos: %w", err)
	}

	if len(videos) == 0 {
		return nil
	}

	var watermark domain.YouTubeContentWatermark
	err = p.db.WithContext(ctx).Where(
		"channel_id = ? AND watermark_type = ?",
		channelID, domain.WatermarkTypeVideo,
	).First(&watermark).Error

	isInitialized := err == nil && watermark.Initialized
	lastSeenID := watermark.LastContentID

	newVideos := make([]*scraper.Video, 0, len(videos))
	for _, video := range videos {
		if isInitialized && video.VideoID == lastSeenID {
			break
		}
		newVideos = append(newVideos, video)
	}

	dbVideos := make([]*domain.YouTubeVideo, 0, len(newVideos))
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, len(newVideos))
	for _, video := range newVideos {
		isLiveReplay := isLiveReplayVideo(video.PublishedText)
		thumbnails := convertThumbnails(video.Thumbnail)

		dbVideo := &domain.YouTubeVideo{
			VideoID:       video.VideoID,
			ChannelID:     channelID,
			Title:         video.Title,
			Thumbnail:     thumbnails,
			Duration:      video.Duration,
			PublishedText: video.PublishedText,
			IsShort:       false,
			IsLiveReplay:  isLiveReplay,
			ViewCount:     video.ViewCount,
		}

		dbVideos = append(dbVideos, dbVideo)

		if isInitialized && !isLiveReplay {
			notifications = append(notifications, &domain.YouTubeNotificationOutbox{
				Kind:      domain.OutboxKindNewVideo,
				ChannelID: channelID,
				ContentID: video.VideoID,
				Payload:   mustMarshalJSON(dbVideo),
				Status:    domain.OutboxStatusPending,
			})
		}
	}

	if err := p.repo.PersistVideos(ctx, dbVideos, notifications, nil, &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: videos[0].VideoID,
	}); err != nil {
		return fmt.Errorf("persist video batch: %w", err)
	}

	return nil
}

type LivePoller struct {
	client *scraper.Client
	db     *gorm.DB
}

func NewLivePoller(scraperClient *scraper.Client, db *gorm.DB) *LivePoller {
	return &LivePoller{
		client: scraperClient,
		db:     db,
	}
}

func (p *LivePoller) Name() string {
	return "live"
}

func (p *LivePoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *LivePoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *LivePoller) Poll(ctx context.Context, channelID string) error {
	events, err := p.client.GetUpcomingEvents(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get upcoming events: %w", err)
	}

	now := time.Now()

	for _, event := range events {
		var status domain.LiveStatus
		switch event.Status {
		case "LIVE":
			status = domain.LiveStatusLive
		case "UPCOMING":
			status = domain.LiveStatusUpcoming
		default:
			continue // LIVE나 UPCOMING이 아니면 스킵
		}

		// 스케줄 시작 시간
		var scheduledStart *time.Time
		if event.StartTime != nil {
			t := time.Unix(*event.StartTime, 0)
			scheduledStart = &t
		}

		// 라이브 세션 upsert
		session := &domain.YouTubeLiveSession{
			VideoID:            event.VideoID,
			ChannelID:          channelID,
			Status:             status,
			Title:              event.Title,
			ScheduledStartTime: scheduledStart,
		}

		// LIVE 상태면 시작 시간 기록
		if status == domain.LiveStatusLive {
			session.StartedAt = &now
		}

		p.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "video_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"status", "title", "scheduled_start_time", "started_at", "last_seen_at"}),
		}).Create(session)

		// LIVE 상태면 시청자 샘플 저장
		if status == domain.LiveStatusLive {
			viewerCount := parseViewerCount(event.ViewCountText)
			if viewerCount > 0 {
				sample := &domain.YouTubeLiveViewerSample{
					VideoID:           event.VideoID,
					CapturedAt:        now,
					ChannelID:         channelID,
					ConcurrentViewers: viewerCount,
				}

				p.db.WithContext(ctx).Clauses(clause.OnConflict{
					DoNothing: true,
				}).Create(sample)

				slog.Debug("Live viewer sample saved",
					"video_id", event.VideoID,
					"viewers", viewerCount)
			}
		}
	}

	// 더 이상 보이지 않는 LIVE 세션을 ENDED로 전환
	p.markEndedSessions(ctx, channelID, events)

	return nil
}

// markEndedSessions: 종료된 세션 마킹
func (p *LivePoller) markEndedSessions(ctx context.Context, channelID string, currentEvents []*scraper.UpcomingEvent) {
	// 현재 활성 비디오 ID 수집
	activeIDs := make(map[string]bool)
	for _, e := range currentEvents {
		if e.Status == "LIVE" || e.Status == "UPCOMING" {
			activeIDs[e.VideoID] = true
		}
	}

	// DB에서 해당 채널의 LIVE 세션 조회
	var liveSessions []domain.YouTubeLiveSession
	p.db.WithContext(ctx).Where(
		"channel_id = ? AND status = ?",
		channelID, domain.LiveStatusLive,
	).Find(&liveSessions)

	now := time.Now()
	for _, session := range liveSessions {
		if !activeIDs[session.VideoID] {
			// 더 이상 LIVE가 아님 - ENDED로 전환
			p.db.WithContext(ctx).Model(&session).Updates(map[string]any{
				"status":   domain.LiveStatusEnded,
				"ended_at": now,
			})

			// 스트림 통계 집계
			p.finalizeStreamStats(ctx, session.VideoID, channelID)
		}
	}
}

// finalizeStreamStats: 스트림 통계 집계
func (p *LivePoller) finalizeStreamStats(ctx context.Context, videoID, channelID string) {
	// 샘플 데이터 집계
	var result struct {
		MaxViewers int
		AvgViewers int
		Count      int
	}

	p.db.WithContext(ctx).Model(&domain.YouTubeLiveViewerSample{}).
		Select("MAX(concurrent_viewers) as max_viewers, AVG(concurrent_viewers) as avg_viewers, COUNT(*) as count").
		Where("video_id = ?", videoID).
		Scan(&result)

	if result.Count == 0 {
		return
	}

	// 스트림 통계 저장
	var session domain.YouTubeLiveSession
	p.db.WithContext(ctx).Where("video_id = ?", videoID).First(&session)

	stats := &domain.YouTubeStreamStats{
		VideoID:              videoID,
		ChannelID:            channelID,
		StartedAt:            session.StartedAt,
		EndedAt:              session.EndedAt,
		MaxConcurrentViewers: result.MaxViewers,
		AvgConcurrentViewers: result.AvgViewers,
		SampleCount:          result.Count,
	}

	p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "video_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"ended_at", "max_concurrent_viewers", "avg_concurrent_viewers", "sample_count", "updated_at"}),
	}).Create(stats)

	slog.Info("Stream stats finalized",
		"video_id", videoID,
		"max_viewers", result.MaxViewers,
		"avg_viewers", result.AvgViewers)
}

func shouldEnqueueRoutedNotification(
	routeDecider NotificationRouteDecider,
	alarmType domain.AlarmType,
	channelID string,
	publishedAt time.Time,
) bool {
	if routeDecider == nil {
		return true
	}
	return routeDecider(NotificationRouteRequest{
		AlarmType:   alarmType,
		ChannelID:   channelID,
		PublishedAt: yttimestamp.Normalize(publishedAt),
	})
}

func observeCommunityShortsDetectionBatch(ctx context.Context, channelID string, alarmType domain.AlarmType, detectedCount int, detectedAt time.Time) {
	if detectedCount <= 0 {
		return
	}

	ensureMetrics()
	communityShortsDetectedPostsTotal.WithLabelValues(channelID, string(alarmType)).Add(float64(detectedCount))
	slog.LogAttrs(ctx, slog.LevelInfo, logschema.CommunityShortsDetectionBatchMessage,
		slog.String(logschema.FieldChannelID, channelID),
		slog.String(logschema.FieldAlarmType, string(alarmType)),
		slog.Int(logschema.FieldDetectedCount, detectedCount),
		slog.String(logschema.FieldDetectedAt, yttimestamp.Format(detectedAt)),
	)
}

type ShortsPoller struct {
	client                           *scraper.Client
	db                               *gorm.DB
	repo                             batchRepository
	maxResults                       int
	routeDecider                     NotificationRouteDecider
	inlinePublishedAtFallbackEnabled bool
}

func NewShortsPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int, routeDecider NotificationRouteDecider, inlinePublishedAtFallbackEnabled ...bool) *ShortsPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	inlineFallbackEnabled := false
	if len(inlinePublishedAtFallbackEnabled) > 0 {
		inlineFallbackEnabled = inlinePublishedAtFallbackEnabled[0]
	}
	return &ShortsPoller{
		client:                           scraperClient,
		db:                               db,
		repo:                             newBatchRepository(db),
		maxResults:                       maxResults,
		routeDecider:                     routeDecider,
		inlinePublishedAtFallbackEnabled: inlineFallbackEnabled,
	}
}

func (p *ShortsPoller) Name() string {
	return "shorts"
}

func (p *ShortsPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *ShortsPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *ShortsPoller) Poll(ctx context.Context, channelID string) error {
	shorts, err := p.client.GetShorts(ctx, channelID, p.maxResults)
	if err != nil {
		return fmt.Errorf("failed to get shorts: %w", err)
	}

	shorts = normalizeCollectedShortsByCanonicalPostID(shorts)
	if len(shorts) == 0 {
		return nil
	}

	var watermark domain.YouTubeContentWatermark
	err = p.db.WithContext(ctx).Where(
		"channel_id = ? AND watermark_type = ?",
		channelID, domain.WatermarkTypeShort,
	).First(&watermark).Error

	isInitialized := err == nil && watermark.Initialized
	newShorts := make([]*scraper.Short, 0, len(shorts))
	for _, short := range shorts {
		canonicalPostID := normalizeContentID(domain.OutboxKindNewShort, short.VideoID)
		if isInitialized && canonicalPostID == normalizeContentID(domain.OutboxKindNewShort, watermark.LastContentID) {
			break
		}
		newShorts = append(newShorts, short)
	}

	dbVideos := make([]*domain.YouTubeVideo, 0, len(newShorts))
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, len(newShorts))
	trackingRows := make([]*domain.YouTubeContentAlarmTracking, 0, len(newShorts))
	detectedAt := yttimestamp.Normalize(time.Now())
	keepExistingWatermark := false
	observeCommunityShortsDetectionBatch(ctx, channelID, domain.AlarmTypeShorts, len(newShorts), detectedAt)
	for _, short := range newShorts {
		canonicalPostID := normalizeContentID(domain.OutboxKindNewShort, short.VideoID)
		resourceVideoID := normalizeShortVideoResourceID(short.VideoID)
		publishedAt := yttimestamp.NormalizePtr(short.PublishedAt)
		if isInitialized && publishedAt == nil && p.inlinePublishedAtFallbackEnabled {
			publishedAt = p.resolveShortPublishedAtInline(ctx, resourceVideoID)
		}
		thumbnails := convertThumbnails(short.Thumbnail)

		dbVideo := &domain.YouTubeVideo{
			VideoID:     resourceVideoID,
			ChannelID:   channelID,
			Title:       short.Title,
			Thumbnail:   thumbnails,
			PublishedAt: publishedAt,
			IsShort:     true,
			ViewCount:   short.ViewCount,
		}

		dbVideos = append(dbVideos, dbVideo)

		if isInitialized {
			logShortDetected(ctx, channelID, canonicalPostID, dbVideo.PublishedAt, detectedAt)

			trackingRows = append(trackingRows, &domain.YouTubeContentAlarmTracking{
				Kind:              domain.OutboxKindNewShort,
				ContentID:         canonicalPostID,
				ChannelID:         channelID,
				ActualPublishedAt: dbVideo.PublishedAt,
				DetectedAt:        detectedAt,
			})

			var routePublishedAt time.Time
			if dbVideo.PublishedAt != nil {
				routePublishedAt = *dbVideo.PublishedAt
			}
			if p.routeDecider != nil && routePublishedAt.IsZero() {
				if p.inlinePublishedAtFallbackEnabled {
					keepExistingWatermark = true
				}
				continue
			}

			if p.routeDecider == nil || shouldEnqueueRoutedNotification(p.routeDecider, domain.AlarmTypeShorts, channelID, routePublishedAt) {
				notifications = append(notifications, &domain.YouTubeNotificationOutbox{
					Kind:      domain.OutboxKindNewShort,
					ChannelID: channelID,
					ContentID: canonicalPostID,
					Payload:   buildShortNotificationPayload(dbVideo, canonicalPostID),
					Status:    domain.OutboxStatusPending,
				})
			}
		} else {
			logShortDetected(ctx, channelID, canonicalPostID, dbVideo.PublishedAt, detectedAt)
		}
	}
	lastContentID := normalizeContentID(domain.OutboxKindNewShort, shorts[0].VideoID)
	if keepExistingWatermark && strings.TrimSpace(watermark.LastContentID) != "" {
		lastContentID = watermark.LastContentID
	}

	if err := p.repo.PersistVideos(ctx, dbVideos, notifications, trackingRows, &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: lastContentID,
	}); err != nil {
		return fmt.Errorf("persist short batch: %w", err)
	}

	return nil
}

func (p *ShortsPoller) resolveShortPublishedAtInline(ctx context.Context, videoID string) *time.Time {
	if strings.TrimSpace(videoID) == "" {
		return nil
	}

	resolveCtx, cancel := context.WithTimeout(ctx, inlinePublishedAtFallbackTimeout)
	defer cancel()

	publishedAt, err := p.client.ResolveVideoPublishedAt(resolveCtx, videoID)
	if err != nil {
		if errors.Is(err, scraper.ErrPublishedAtNotFound) {
			return nil
		}
		slog.WarnContext(ctx, "short published_at inline fallback failed",
			"video_id", videoID,
			"error", err,
		)
		return nil
	}

	return yttimestamp.NormalizePtr(publishedAt)
}

func logShortDetected(ctx context.Context, channelID, postID string, actualPublishedAt *time.Time, detectedAt time.Time) {
	slog.LogAttrs(ctx, slog.LevelInfo, shortDetectedLogMessage,
		slog.String(logschema.FieldChannelID, channelID),
		slog.String(logschema.FieldPostID, postID),
		optionalTimestampAttr(logschema.FieldActualPublishedAt, actualPublishedAt),
		slog.String(logschema.FieldDetectedAt, yttimestamp.Format(detectedAt)),
	)
}

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
