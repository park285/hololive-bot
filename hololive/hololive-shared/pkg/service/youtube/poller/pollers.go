package poller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// ChannelStatsPoller: 채널 통계 폴러
type ChannelStatsPoller struct {
	client          *scraper.Client
	db              *gorm.DB
	profileCacheTTL time.Duration
}

// NewChannelStatsPoller: 새 채널 통계 폴러 생성
func NewChannelStatsPoller(scraperClient *scraper.Client, db *gorm.DB) *ChannelStatsPoller {
	return &ChannelStatsPoller{
		client:          scraperClient,
		db:              db,
		profileCacheTTL: 24 * time.Hour,
	}
}

// Name: 폴러 이름 반환
func (p *ChannelStatsPoller) Name() string {
	return "channel_stats"
}

// SetProxyEnabled: 런타임 프록시 토글
func (p *ChannelStatsPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

// ProxyEnabled: 현재 프록시 활성 상태
func (p *ChannelStatsPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

// Poll: 채널 통계 폴링 수행
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

// VideosPoller: 새 영상 감지 폴러
type VideosPoller struct {
	client     *scraper.Client
	db         *gorm.DB
	maxResults int
}

// NewVideosPoller: 새 영상 폴러 생성
func NewVideosPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int) *VideosPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	return &VideosPoller{
		client:     scraperClient,
		db:         db,
		maxResults: maxResults,
	}
}

// Name: 폴러 이름 반환
func (p *VideosPoller) Name() string {
	return "videos"
}

// SetProxyEnabled: 런타임 프록시 토글
func (p *VideosPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

// ProxyEnabled: 현재 프록시 활성 상태
func (p *VideosPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

// Poll: 새 영상 폴링 수행
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

		result := p.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "video_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"last_seen_at", "view_count"}),
		}).Create(dbVideo)

		if result.Error != nil {
			slog.Warn("Failed to upsert video",
				"video_id", video.VideoID,
				"error", result.Error)
			continue
		}

		if isInitialized && !isLiveReplay {
			p.enqueueNotification(ctx, channelID, video.VideoID, domain.OutboxKindNewVideo, dbVideo)
		}
	}

	p.updateWatermark(ctx, channelID, domain.WatermarkTypeVideo, videos[0].VideoID)

	return nil
}

// enqueueNotification: 알림 outbox에 추가
func (p *VideosPoller) enqueueNotification(ctx context.Context, channelID, contentID string, kind domain.OutboxKind, payload any) {
	// 중복 방지: ON CONFLICT DO NOTHING
	outbox := &domain.YouTubeNotificationOutbox{
		Kind:      kind,
		ChannelID: channelID,
		ContentID: contentID,
		Payload:   mustMarshalJSON(payload),
		Status:    domain.OutboxStatusPending,
	}

	result := p.db.WithContext(ctx).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(outbox)

	if result.Error != nil {
		slog.Warn("Failed to enqueue notification",
			"kind", kind,
			"content_id", contentID,
			"error", result.Error)
	}
}

// updateWatermark: 워터마크 업데이트
func (p *VideosPoller) updateWatermark(ctx context.Context, channelID string, wmType domain.WatermarkType, lastContentID string) {
	watermark := &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: wmType,
		Initialized:   true,
		LastContentID: lastContentID,
	}

	p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "watermark_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"initialized", "last_content_id", "updated_at"}),
	}).Create(watermark)
}

// LivePoller: 라이브 상태 및 시청자 샘플 폴러
type LivePoller struct {
	client *scraper.Client
	db     *gorm.DB
}

// NewLivePoller: 새 라이브 폴러 생성
func NewLivePoller(scraperClient *scraper.Client, db *gorm.DB) *LivePoller {
	return &LivePoller{
		client: scraperClient,
		db:     db,
	}
}

// Name: 폴러 이름 반환
func (p *LivePoller) Name() string {
	return "live"
}

// SetProxyEnabled: 런타임 프록시 토글
func (p *LivePoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

// ProxyEnabled: 현재 프록시 활성 상태
func (p *LivePoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

// Poll: 라이브 상태 폴링 수행
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

// ShortsPoller: 쇼츠 감지 폴러
type ShortsPoller struct {
	client     *scraper.Client
	db         *gorm.DB
	maxResults int
}

// NewShortsPoller: 새 쇼츠 폴러 생성
func NewShortsPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int) *ShortsPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	return &ShortsPoller{
		client:     scraperClient,
		db:         db,
		maxResults: maxResults,
	}
}

// Name: 폴러 이름 반환
func (p *ShortsPoller) Name() string {
	return "shorts"
}

// SetProxyEnabled: 런타임 프록시 토글
func (p *ShortsPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

// ProxyEnabled: 현재 프록시 활성 상태
func (p *ShortsPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

// Poll: 쇼츠 폴링 수행
func (p *ShortsPoller) Poll(ctx context.Context, channelID string) error {
	shorts, err := p.client.GetShorts(ctx, channelID, p.maxResults)
	if err != nil {
		return fmt.Errorf("failed to get shorts: %w", err)
	}

	if len(shorts) == 0 {
		return nil
	}

	var watermark domain.YouTubeContentWatermark
	err = p.db.WithContext(ctx).Where(
		"channel_id = ? AND watermark_type = ?",
		channelID, domain.WatermarkTypeShort,
	).First(&watermark).Error

	isInitialized := err == nil && watermark.Initialized
	lastSeenID := watermark.LastContentID

	newShorts := make([]*scraper.Short, 0, len(shorts))
	for _, short := range shorts {
		if isInitialized && short.VideoID == lastSeenID {
			break
		}
		newShorts = append(newShorts, short)
	}

	for _, short := range newShorts {
		thumbnails := convertThumbnails(short.Thumbnail)

		dbVideo := &domain.YouTubeVideo{
			VideoID:   short.VideoID,
			ChannelID: channelID,
			Title:     short.Title,
			Thumbnail: thumbnails,
			IsShort:   true,
			ViewCount: short.ViewCount,
		}

		result := p.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "video_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"last_seen_at", "view_count"}),
		}).Create(dbVideo)

		if result.Error != nil {
			slog.Warn("Failed to upsert short",
				"video_id", short.VideoID,
				"error", result.Error)
			continue
		}

		if isInitialized {
			p.enqueueNotification(ctx, channelID, short.VideoID, domain.OutboxKindNewShort, dbVideo)
		}
	}

	p.updateWatermark(ctx, channelID, domain.WatermarkTypeShort, shorts[0].VideoID)

	return nil
}

// enqueueNotification: 알림 outbox에 추가
func (p *ShortsPoller) enqueueNotification(ctx context.Context, channelID, contentID string, kind domain.OutboxKind, payload any) {
	outbox := &domain.YouTubeNotificationOutbox{
		Kind:      kind,
		ChannelID: channelID,
		ContentID: contentID,
		Payload:   mustMarshalJSON(payload),
		Status:    domain.OutboxStatusPending,
	}

	result := p.db.WithContext(ctx).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(outbox)

	if result.Error != nil {
		slog.Warn("Failed to enqueue notification",
			"kind", kind,
			"content_id", contentID,
			"error", result.Error)
	}
}

// updateWatermark: 워터마크 업데이트
func (p *ShortsPoller) updateWatermark(ctx context.Context, channelID string, wmType domain.WatermarkType, lastContentID string) {
	watermark := &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: wmType,
		Initialized:   true,
		LastContentID: lastContentID,
	}

	p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "watermark_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"initialized", "last_content_id", "updated_at"}),
	}).Create(watermark)
}

// CommunityPoller: 커뮤니티 포스트 감지 폴러
type CommunityPoller struct {
	client     *scraper.Client
	db         *gorm.DB
	maxResults int
	keywords   []string
}

// NewCommunityPoller: 새 커뮤니티 폴러 생성
func NewCommunityPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int, keywords []string) *CommunityPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	return &CommunityPoller{
		client:     scraperClient,
		db:         db,
		maxResults: maxResults,
		keywords:   keywords,
	}
}

// Name: 폴러 이름 반환
func (p *CommunityPoller) Name() string {
	return "community"
}

// SetProxyEnabled: 런타임 프록시 토글
func (p *CommunityPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

// ProxyEnabled: 현재 프록시 활성 상태
func (p *CommunityPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

// Poll: 커뮤니티 포스트 폴링 수행
func (p *CommunityPoller) Poll(ctx context.Context, channelID string) error {
	posts, err := p.client.GetCommunityPosts(ctx, channelID, p.maxResults)
	if err != nil {
		return fmt.Errorf("failed to get community posts: %w", err)
	}

	if len(posts) == 0 {
		return nil
	}

	var watermark domain.YouTubeContentWatermark
	err = p.db.WithContext(ctx).Where(
		"channel_id = ? AND watermark_type = ?",
		channelID, domain.WatermarkTypeCommunityPost,
	).First(&watermark).Error

	isInitialized := err == nil && watermark.Initialized
	lastSeenID := watermark.LastContentID

	newPosts := make([]*scraper.CommunityPost, 0, len(posts))
	for _, post := range posts {
		if isInitialized && post.PostID == lastSeenID {
			break
		}
		newPosts = append(newPosts, post)
	}

	for _, post := range newPosts {
		authorPhoto := convertThumbnails(post.AuthorPhoto)
		images := convertThumbnails(post.Images)

		dbPost := &domain.YouTubeCommunityPost{
			PostID:        post.PostID,
			ChannelID:     channelID,
			AuthorName:    post.AuthorName,
			AuthorPhoto:   authorPhoto,
			ContentText:   post.ContentText,
			PublishedText: post.PublishedText,
			LikeCount:     post.LikeCount,
			CommentCount:  post.CommentCount,
			Images:        images,
			AttachedVideo: post.VideoID,
		}

		result := p.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "post_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"last_seen_at", "like_count", "comment_count"}),
		}).Create(dbPost)

		if result.Error != nil {
			slog.Warn("Failed to upsert community post",
				"post_id", post.PostID,
				"error", result.Error)
			continue
		}

		if isInitialized && p.matchesKeywords(post.ContentText) {
			p.enqueueNotification(ctx, channelID, post.PostID, domain.OutboxKindCommunityPost, dbPost)
		}
	}

	p.updateWatermark(ctx, channelID, domain.WatermarkTypeCommunityPost, posts[0].PostID)

	return nil
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

// enqueueNotification: 알림 outbox에 추가
func (p *CommunityPoller) enqueueNotification(ctx context.Context, channelID, contentID string, kind domain.OutboxKind, payload any) {
	outbox := &domain.YouTubeNotificationOutbox{
		Kind:      kind,
		ChannelID: channelID,
		ContentID: contentID,
		Payload:   mustMarshalJSON(payload),
		Status:    domain.OutboxStatusPending,
	}

	result := p.db.WithContext(ctx).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(outbox)

	if result.Error != nil {
		slog.Warn("Failed to enqueue notification",
			"kind", kind,
			"content_id", contentID,
			"error", result.Error)
	}
}

// updateWatermark: 워터마크 업데이트
func (p *CommunityPoller) updateWatermark(ctx context.Context, channelID string, wmType domain.WatermarkType, lastContentID string) {
	watermark := &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: wmType,
		Initialized:   true,
		LastContentID: lastContentID,
	}

	p.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "watermark_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"initialized", "last_content_id", "updated_at"}),
	}).Create(watermark)
}
