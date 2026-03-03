package youtube

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/api/youtube/v3"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// StatsService: 상위 수준의 통계 서비스로, Repository와 YouTube Service를 조합하여 통계 데이터를 제공합니다.
type StatsService struct {
	oauth     *OAuthService
	cache     cache.Client
	statsRepo StatsServiceRepository
	logger    *slog.Logger
}

const (
	channelStatsCachePrefix = "youtube:stats:last:"
	channelStatsCacheTTL    = 10 * time.Minute
)

// ChannelStatistics: 채널의 구독자 수, 비디오 수, 조회 수 정보를 담는 구조체 (변화량 포함)
type ChannelStatistics struct {
	ChannelID        string
	SubscriberCount  uint64
	SubscriberChange int64
	VideoCount       uint64
	ViewCount        uint64
}

// NewStatsService: 통계 서비스 인스턴스를 생성합니다.
func NewStatsService(oauth *OAuthService, cacheSvc cache.Client, statsRepo StatsServiceRepository, logger *slog.Logger) *StatsService {
	return &StatsService{
		oauth:     oauth,
		cache:     cacheSvc,
		statsRepo: statsRepo,
		logger:    logger,
	}
}

// 멤버 캐시를 channelID → memberName 맵으로 변환
func (ys *StatsService) loadMemberLookup(ctx context.Context) map[string]string {
	if ys.cache == nil {
		return nil
	}

	memberMap, err := ys.cache.GetAllMembers(ctx)
	if err != nil {
		ys.logger.Warn("Failed to fetch member cache", slog.Any("error", err))
		return nil
	}

	if len(memberMap) == 0 {
		return nil
	}

	lookup := make(map[string]string, len(memberMap))
	for key, channelID := range memberMap {
		// key format: "name:org" → name 부분만 추출
		name := key
		if idx := strings.LastIndex(key, ":"); idx > 0 {
			name = key[:idx]
		}
		lookup[channelID] = name
	}
	return lookup
}

// 캐시 우선, DB 폴백으로 이전 통계 로드
func (ys *StatsService) loadPreviousStats(ctx context.Context, channelID string) *domain.TimestampedStats {
	if ys.cache != nil {
		cacheKey := channelStatsCachePrefix + channelID
		var cached domain.TimestampedStats
		if err := ys.cache.Get(ctx, cacheKey, &cached); err == nil && cached.ChannelID != "" {
			return new(cached)
		}
	}

	if ys.statsRepo != nil {
		dbPrev, err := ys.statsRepo.GetLatestStats(ctx, channelID)
		if err == nil {
			return dbPrev
		}
		ys.logger.Warn("Failed to load previous stats",
			slog.String("channel", channelID),
			slog.Any("error", err))
	}

	return nil
}

// lookup과 이전 통계에서 멤버 이름 결정
func (ys *StatsService) determineMemberName(channelID string, prevStats *domain.TimestampedStats, memberLookup map[string]string) string {
	if memberLookup != nil {
		if name := memberLookup[channelID]; name != "" {
			return name
		}
	}

	if prevStats != nil && prevStats.MemberName != "" {
		return prevStats.MemberName
	}

	return ""
}

// 현재 통계를 DB와 캐시에 저장하고 구독자 변화량 반환
func (ys *StatsService) saveCurrentStats(ctx context.Context, item *youtube.Channel, memberName string, prevStats *domain.TimestampedStats) int64 {
	now := time.Now()
	currentStats := &domain.TimestampedStats{
		ChannelID:       item.Id,
		MemberName:      memberName,
		SubscriberCount: item.Statistics.SubscriberCount,
		VideoCount:      item.Statistics.VideoCount,
		ViewCount:       item.Statistics.ViewCount,
		Timestamp:       now,
	}

	var subscriberChange int64
	if prevStats != nil {
		subscriberChange = int64(item.Statistics.SubscriberCount) - int64(prevStats.SubscriberCount)
	}

	if ys.statsRepo != nil {
		if err := ys.statsRepo.SaveStats(ctx, currentStats); err != nil {
			ys.logger.Warn("Failed to persist current stats snapshot",
				slog.String("channel", item.Id),
				slog.Any("error", err))
		}
	}

	if ys.cache != nil {
		cacheKey := channelStatsCachePrefix + item.Id
		_ = ys.cache.Set(ctx, cacheKey, currentStats, channelStatsCacheTTL)
	}

	return subscriberChange
}

// 개별 채널 아이템 처리
func (ys *StatsService) processBatchItem(ctx context.Context, item *youtube.Channel, memberLookup map[string]string) *ChannelStatistics {
	channelStat := &ChannelStatistics{
		ChannelID:       item.Id,
		SubscriberCount: item.Statistics.SubscriberCount,
		VideoCount:      item.Statistics.VideoCount,
		ViewCount:       item.Statistics.ViewCount,
	}

	prevStats := ys.loadPreviousStats(ctx, item.Id)
	memberName := ys.determineMemberName(item.Id, prevStats, memberLookup)
	channelStat.SubscriberChange = ys.saveCurrentStats(ctx, item, memberName, prevStats)

	return channelStat
}

// GetChannelStatisticsBatch: 여러 채널의 통계를 배치(최대 50개)로 조회한다. (OAuth 인증 필요)
func (ys *StatsService) GetChannelStatisticsBatch(ctx context.Context, channelIDs []string) ([]*ChannelStatistics, error) {
	if ys.oauth == nil || !ys.oauth.IsAuthorized() {
		return nil, fmt.Errorf("YouTube OAuth not authorized")
	}

	service := ys.oauth.GetService()
	if service == nil {
		return nil, fmt.Errorf("YouTube service not available")
	}

	var stats []*ChannelStatistics
	memberLookup := ys.loadMemberLookup(ctx)

	const maxPerRequest = 50
	for i := 0; i < len(channelIDs); i += maxPerRequest {
		end := min(i+maxPerRequest, len(channelIDs))

		batch := channelIDs[i:end]
		call := service.Channels.List([]string{"statistics"}).Id(batch...)

		resp, err := call.Context(ctx).Do()
		if err != nil {
			ys.logger.Error("Failed to get channel statistics",
				slog.Int("batch_size", len(batch)),
				slog.Any("error", err))
			continue
		}

		for _, item := range resp.Items {
			channelStat := ys.processBatchItem(ctx, item, memberLookup)
			stats = append(stats, channelStat)
		}
	}

	return stats, nil
}

// GetRecentVideos: 채널의 최근 업로드 비디오를 검색한다. (OAuth 인증 필요)
func (ys *StatsService) GetRecentVideos(ctx context.Context, channelID string, maxResults int64) ([]*youtube.SearchResult, error) {
	if ys.oauth == nil || !ys.oauth.IsAuthorized() {
		return nil, fmt.Errorf("YouTube OAuth not authorized")
	}

	service := ys.oauth.GetService()
	if service == nil {
		return nil, fmt.Errorf("YouTube service not available")
	}

	call := service.Search.List([]string{"id", "snippet"}).
		ChannelId(channelID).
		Type("video").
		Order("date").
		MaxResults(maxResults)

	resp, err := call.Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to search videos: %w", err)
	}

	return resp.Items, nil
}
