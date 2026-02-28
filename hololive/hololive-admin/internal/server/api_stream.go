package server

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

const channelStatsCacheKey = "admin:channel_stats"
const channelStatsCacheTTL = 10 * time.Minute
const channelStatsRefreshLockKey = "admin:channel_stats:refresh_lock"
const channelStatsRefreshLockValue = "locked"
const channelStatsRefreshLockTTL = 5 * time.Minute
const channelStatsCacheWorkers = 4
const channelStatsRefreshWorkers = 1
const memberIndexCacheTTL = 1 * time.Minute

// GetLiveStreams: 현재 라이브 방송 중인 스트림 목록을 반환합니다.
func (h *StreamAPIHandler) GetLiveStreams(c *gin.Context) {
	ctx := c.Request.Context()
	org := constants.HolodexAPIParams.OrgHololive
	if rawOrg, hasOrg := c.GetQuery("org"); hasOrg {
		org = strings.TrimSpace(rawOrg)
		if org == "" {
			h.respondError(c, 400, "Invalid org parameter", gin.H{
				"default_org":    strings.ToLower(constants.HolodexAPIParams.OrgHololive),
				"supported_orgs": holodex.SupportedStreamOrgParams(),
			})
			return
		}
	}

	streams, err := h.holodex.GetLiveStreamsByOrg(ctx, org)
	if err != nil {
		if stdErrors.Is(err, holodex.ErrInvalidStreamOrg) {
			h.respondError(c, 400, "Invalid org parameter", gin.H{
				"default_org":    strings.ToLower(constants.HolodexAPIParams.OrgHololive),
				"supported_orgs": holodex.SupportedStreamOrgParams(),
			})
			return
		}
		h.respondInternalError(c, "Failed to get live streams", "Failed to get live streams", err)
		return
	}
	c.JSON(200, gin.H{"status": "ok", "org": org, "streams": streams})
}

// GetUpcomingStreams: 예정된 스트림 목록을 반환합니다.
func (h *StreamAPIHandler) GetUpcomingStreams(c *gin.Context) {
	ctx := c.Request.Context()
	org := constants.HolodexAPIParams.OrgHololive
	if rawOrg, hasOrg := c.GetQuery("org"); hasOrg {
		org = strings.TrimSpace(rawOrg)
		if org == "" {
			h.respondError(c, 400, "Invalid org parameter", gin.H{
				"default_org":    strings.ToLower(constants.HolodexAPIParams.OrgHololive),
				"supported_orgs": holodex.SupportedStreamOrgParams(),
			})
			return
		}
	}

	streams, err := h.holodex.GetUpcomingStreamsByOrg(ctx, 24, org)
	if err != nil {
		if stdErrors.Is(err, holodex.ErrInvalidStreamOrg) {
			h.respondError(c, 400, "Invalid org parameter", gin.H{
				"default_org":    strings.ToLower(constants.HolodexAPIParams.OrgHololive),
				"supported_orgs": holodex.SupportedStreamOrgParams(),
			})
			return
		}
		h.respondInternalError(c, "Failed to get upcoming streams", "Failed to get upcoming streams", err)
		return
	}
	c.JSON(200, gin.H{"status": "ok", "org": org, "streams": streams})
}

// GetChannelStats: 채널 통계를 반환합니다. (SWR 패턴: 캐시 → DB → 백그라운드 갱신)
// 캐시 TTL: 10분, DB 스냅샷은 ChannelStatsPoller가 6시간마다 갱신
func (h *StreamAPIHandler) GetChannelStats(c *gin.Context) {
	ctx := c.Request.Context()

	// 1. 캐시 확인 (빠른 경로)
	if h.valkeyCache != nil {
		var cachedStats map[string]*youtube.ChannelStats
		if err := h.valkeyCache.Get(ctx, channelStatsCacheKey, &cachedStats); err != nil {
			h.respondInternalError(
				c,
				"Failed to get channel stats",
				"Failed to get channel stats from cache",
				err,
			)
			return
		}
		if cachedStats != nil {
			h.logger.Debug("Channel stats cache hit", slog.Int("count", len(cachedStats)))
			c.JSON(200, gin.H{"status": "ok", "stats": cachedStats})
			return
		}
	}

	// 2. 캐시 miss → DB 스냅샷에서 즉시 조회 (SWR: Stale-While-Revalidate)
	if h.statsRepo != nil {
		stats, err := h.getChannelStatsFromDB(ctx)
		if err != nil {
			h.respondInternalError(
				c,
				"Failed to get channel stats",
				"Failed to get channel stats from DB",
				err,
			)
			return
		}
		if len(stats) > 0 {
			h.logger.Debug("Channel stats DB snapshot hit", slog.Int("count", len(stats)))

			// 캐시에 저장 (다음 요청 가속화)
			h.cacheChannelStatsAsync(ctx, stats)

			// 백그라운드 갱신 트리거 (Refresh Lock으로 중복 방지)
			h.triggerChannelStatsRefreshAsync(ctx)

			c.JSON(200, gin.H{"status": "ok", "stats": stats, "source": "db_snapshot"})
			return
		}
	}

	// 3. DB 스냅샷도 없으면 폴러 동기화 전 상태로 간주
	h.respondError(c, 503, "Channel stats snapshot not ready", gin.H{
		"code": "channel_stats_snapshot_not_ready",
		"hint": "retry later after background poller sync",
	})
}

// getChannelStatsFromDB: DB 스냅샷에서 채널 통계를 조회합니다.
// domain.TimestampedStats → youtube.ChannelStats 변환
func (h *StreamAPIHandler) getChannelStatsFromDB(ctx context.Context) (map[string]*youtube.ChannelStats, error) {
	channelIDs, channelToName, err := h.getActiveMemberIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("get members: %w", err)
	}

	if len(channelIDs) == 0 {
		return make(map[string]*youtube.ChannelStats), nil
	}

	dbStats, err := h.statsRepo.GetLatestStatsForChannels(ctx, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("get latest stats: %w", err)
	}

	result := make(map[string]*youtube.ChannelStats, len(dbStats))
	for channelID, ts := range dbStats {
		title := ts.MemberName
		if title == "" {
			title = channelToName[channelID]
		}
		result[channelID] = &youtube.ChannelStats{
			ChannelID:       ts.ChannelID,
			ChannelTitle:    title,
			SubscriberCount: ts.SubscriberCount,
			VideoCount:      ts.VideoCount,
			ViewCount:       ts.ViewCount,
			Timestamp:       ts.Timestamp,
		}
	}

	return result, nil
}

// cacheChannelStatsAsync: 채널 통계를 캐시에 비동기 저장합니다.
func (h *StreamAPIHandler) cacheChannelStatsAsync(ctx context.Context, stats map[string]*youtube.ChannelStats) {
	if h.valkeyCache == nil || stats == nil {
		return
	}

	h.runAsyncWithLimiter(h.channelStatsCacheLimiter, "cache_channel_stats", func() {
		cacheCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			constants.YouTubeConfig.CacheSaveTimeout,
		)
		defer cancel()

		if err := h.valkeyCache.Set(cacheCtx, channelStatsCacheKey, stats, channelStatsCacheTTL); err != nil {
			h.logger.Warn("Failed to cache channel stats", slog.Any("error", err))
		}
	})
}

// triggerChannelStatsRefreshAsync: 백그라운드에서 채널 통계 갱신을 트리거합니다.
// Refresh Lock으로 중복 갱신(캐시 스탬피드)을 방지합니다.
func (h *StreamAPIHandler) triggerChannelStatsRefreshAsync(ctx context.Context) {
	if h.valkeyCache == nil || h.youtube == nil {
		return
	}

	h.runAsyncWithLimiter(h.channelStatsRefreshLimiter, "refresh_channel_stats", func() {
		bgCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			constants.YouTubeConfig.ScraperPhaseTimeout,
		)
		defer cancel()

		// Refresh Lock 획득 시도 (SetNX: 락이 없을 때만 성공)
		acquired, err := h.valkeyCache.SetNX(bgCtx, channelStatsRefreshLockKey, channelStatsRefreshLockValue, channelStatsRefreshLockTTL)
		if err != nil {
			h.logger.Warn("Failed to acquire refresh lock", slog.Any("error", err))
			return
		}
		if !acquired {
			h.logger.Debug("Refresh lock already held, skipping background refresh")
			return
		}

		h.logger.Info("Background channel stats refresh started")

		channelIDs, _, err := h.getActiveMemberIndex(bgCtx)
		if err != nil {
			h.logger.Warn("Background refresh: failed to get members", slog.Any("error", err))
			return
		}

		stats, err := h.youtube.GetChannelStatistics(bgCtx, channelIDs)
		if err != nil {
			h.logger.Warn("Background refresh: failed to get stats", slog.Any("error", err))
			return
		}

		h.cacheChannelStatsAsync(bgCtx, stats)
		h.logger.Info("Background channel stats refresh completed", slog.Int("count", len(stats)))
	})
}

func (h *StreamAPIHandler) runAsyncWithLimiter(limiter chan struct{}, task string, fn func()) {
	if limiter == nil {
		go fn()
		return
	}

	select {
	case limiter <- struct{}{}:
		go func() {
			defer func() { <-limiter }()
			fn()
		}()
	default:
		h.logger.Debug("Skip async task: limiter saturated", slog.String("task", task))
	}
}

func (h *StreamAPIHandler) getActiveMemberIndex(ctx context.Context) ([]string, map[string]string, error) {
	h.memberIndexMu.RLock()
	if h.memberIndexReady && time.Now().Before(h.memberIndexExpiresAt) {
		channelIDs := append([]string(nil), h.memberChannelIDs...)
		channelToName := cloneChannelNameMap(h.memberChannelName)
		h.memberIndexMu.RUnlock()
		return channelIDs, channelToName, nil
	}
	h.memberIndexMu.RUnlock()

	h.memberIndexMu.Lock()
	defer h.memberIndexMu.Unlock()

	if h.memberIndexReady && time.Now().Before(h.memberIndexExpiresAt) {
		channelIDs := append([]string(nil), h.memberChannelIDs...)
		channelToName := cloneChannelNameMap(h.memberChannelName)
		return channelIDs, channelToName, nil
	}

	members, err := h.repo.GetAllMembers(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get all members: %w", err)
	}

	channelIDs := make([]string, 0, len(members))
	channelToName := make(map[string]string, len(members))
	for _, member := range members {
		if member.ChannelID == "" || member.IsGraduated {
			continue
		}
		channelIDs = append(channelIDs, member.ChannelID)
		channelToName[member.ChannelID] = member.Name
	}

	h.memberChannelIDs = append([]string(nil), channelIDs...)
	h.memberChannelName = cloneChannelNameMap(channelToName)
	h.memberIndexExpiresAt = time.Now().Add(memberIndexCacheTTL)
	h.memberIndexReady = true

	return channelIDs, channelToName, nil
}

func (h *APIHandler) invalidateMemberIndex() {
	h.memberIndexMu.Lock()
	defer h.memberIndexMu.Unlock()

	h.memberChannelIDs = nil
	h.memberChannelName = make(map[string]string)
	h.memberIndexExpiresAt = time.Time{}
	h.memberIndexReady = false
}

func cloneChannelNameMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for key, val := range src {
		dst[key] = val
	}
	return dst
}

// GetChannel: channelIds 파라미터로 여러 채널을 한 번에 조회합니다.
// - 배치 조회: /api/holo/channels?channelIds=UC1,UC2,UC3...
// NOTE: DB에서 직접 조회하여 Holodex API 호출 없이 응답합니다.
func (h *StreamAPIHandler) GetChannel(c *gin.Context) {
	ctx := c.Request.Context()

	// 배치 조회 지원: channelIds 파라미터 확인
	channelIDs := c.Query("channelIds")
	if channelIDs != "" {
		ids := splitChannelIDs(channelIDs)
		if len(ids) == 0 {
			h.respondError(c, 400, "channelIds parameter is empty or invalid", nil)
			return
		}

		// 최대 100개로 제한
		if len(ids) > 100 {
			h.respondError(c, 400, "channelIds supports at most 100 values", gin.H{
				"limit":    100,
				"received": len(ids),
			})
			return
		}

		// DB에서 직접 조회 (Holodex API 호출 없음)
		channelsMap, err := h.repo.GetMembersWithPhoto(ctx, ids)
		if err != nil {
			h.respondInternalError(
				c,
				"Failed to get channels",
				"Failed to get channels from DB",
				err,
				slog.Int("count", len(ids)),
			)
			return
		}

		// Map을 슬라이스로 변환
		channels := make([]*ChannelResponse, 0, len(channelsMap))
		for _, member := range channelsMap {
			channels = append(channels, memberToChannelResponse(member))
		}

		c.JSON(200, gin.H{"status": "ok", "channels": channels})
		return
	}

	// 레거시 단일 조회는 제거됨
	channelID := c.Query("channelId")
	if channelID != "" {
		h.respondError(c, 410, "Legacy channelId query is no longer supported", gin.H{
			"hint": "use channelIds query parameter",
		})
		return
	}
	h.respondError(c, 400, "channelIds parameter required", nil)
}

// ChannelResponse: 채널 API 응답 구조체 (기존 Holodex 호환 형식)
type ChannelResponse struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Photo *string `json:"photo,omitempty"`
}

// memberToChannelResponse: domain.Member를 API 응답 형식으로 변환
func memberToChannelResponse(m *domain.Member) *ChannelResponse {
	if m == nil {
		return nil
	}
	resp := &ChannelResponse{
		ID:   m.ChannelID,
		Name: m.Name,
	}
	if m.Photo != "" {
		resp.Photo = &m.Photo
	}
	return resp
}

// splitChannelIDs: 쉼표로 구분된 채널 ID 문자열을 슬라이스로 분리합니다.
func splitChannelIDs(ids string) []string {
	parts := make([]string, 0)
	for _, id := range splitByComma(ids) {
		trimmed := trimSpace(id)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func splitByComma(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// SearchChannels: 이름으로 채널을 검색합니다.
func (h *StreamAPIHandler) SearchChannels(c *gin.Context) {
	ctx := c.Request.Context()
	query := c.Query("q")
	if query == "" {
		c.JSON(400, gin.H{"error": "q parameter required"})
		return
	}

	channels, err := h.holodex.SearchChannels(ctx, query)
	if err != nil {
		h.logger.Error("Failed to search channels", slog.String("query", query), slog.Any("error", err))
		c.JSON(500, gin.H{"error": "Failed to search channels"})
		return
	}

	c.JSON(200, gin.H{"status": "ok", "channels": channels})
}
