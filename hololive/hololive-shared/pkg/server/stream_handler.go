package server

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

const (
	ChannelStatsCacheKey = "admin:channel_stats"
	ChannelStatsCacheTTL = 10 * time.Minute

	ChannelStatsRefreshLockKey   = "admin:channel_stats:refresh_lock"
	ChannelStatsRefreshLockValue = "locked"
	ChannelStatsRefreshLockTTL   = 5 * time.Minute

	MemberIndexCacheTTL = 1 * time.Minute

	DefaultChannelStatsCacheWorkers   = 4
	DefaultChannelStatsRefreshWorkers = 1
)

type memberIndexSnapshot struct {
	channelIDs   []string
	channelNames map[string]string
}

// StreamState: stream/stat 조회 경로에서 사용하는 내부 상태 캐시입니다.
type StreamState struct {
	channelStatsCacheLimiter   chan struct{}
	channelStatsRefreshLimiter chan struct{}
	memberIndexMu              sync.RWMutex
	memberIndexExpiresAt       time.Time
	memberChannelIDs           []string
	memberChannelName          map[string]string
	memberIndexReady           bool
	memberIndexBuildGroup      singleflight.Group
}

// NewStreamState: stream/stat 조회용 상태 캐시를 생성합니다.
func NewStreamState(cacheWorkers, refreshWorkers int) *StreamState {
	state := &StreamState{
		memberChannelName: make(map[string]string),
	}
	if cacheWorkers > 0 {
		state.channelStatsCacheLimiter = make(chan struct{}, cacheWorkers)
	}
	if refreshWorkers > 0 {
		state.channelStatsRefreshLimiter = make(chan struct{}, refreshWorkers)
	}
	return state
}

// InvalidateMemberIndex: 멤버 인덱스 캐시를 무효화합니다.
func (s *StreamState) InvalidateMemberIndex() {
	s.memberIndexMu.Lock()
	defer s.memberIndexMu.Unlock()

	s.memberChannelIDs = nil
	s.memberChannelName = make(map[string]string)
	s.memberIndexExpiresAt = time.Time{}
	s.memberIndexReady = false
}

// StreamMemberRepository: 채널 배치 조회에서 사용하는 최소 인터페이스입니다.
type StreamMemberRepository interface {
	GetMembersWithPhoto(ctx context.Context, channelIDs []string) (map[string]*domain.Member, error)
}

// StreamRespondErrorFunc: API 에러 응답 함수 시그니처입니다.
type StreamRespondErrorFunc func(c *gin.Context, status int, message string, extra gin.H)

// StreamRespondInternalErrorFunc: 내부 에러 응답 함수 시그니처입니다.
type StreamRespondInternalErrorFunc func(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr)

// StreamHandler: stream API 공통 HTTP 핸들러 로직을 제공합니다.
type StreamHandler struct {
	Logger               *slog.Logger
	Holodex              *holodex.Service
	YouTube              *youtube.Service
	ValkeyCache          *cache.Service
	StatsRepo            youtube.StatsDashboardRepository
	MemberRepo           StreamMemberRepository
	MemberIndexLoader    func(context.Context) ([]*domain.Member, error)
	State                *StreamState
	RespondError         StreamRespondErrorFunc
	RespondInternalError StreamRespondInternalErrorFunc
}

func (h *StreamHandler) ensureState() *StreamState {
	if h.State == nil {
		h.State = NewStreamState(DefaultChannelStatsCacheWorkers, DefaultChannelStatsRefreshWorkers)
	}
	return h.State
}

func (h *StreamHandler) respondError(c *gin.Context, status int, message string, extra gin.H) {
	if h.RespondError != nil {
		h.RespondError(c, status, message, extra)
		return
	}
	RespondError(c, status, message, extra)
}

func (h *StreamHandler) respondInternalError(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr) {
	if h.RespondInternalError != nil {
		h.RespondInternalError(c, userMessage, logMessage, err, attrs...)
		return
	}
	RespondInternalError(h.Logger, c, userMessage, logMessage, err, attrs...)
}

// GetLiveStreams: 현재 라이브 방송 중인 스트림 목록을 반환합니다.
func (h *StreamHandler) GetLiveStreams(c *gin.Context) {
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

	streams, err := h.Holodex.GetLiveStreamsByOrg(ctx, org)
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
func (h *StreamHandler) GetUpcomingStreams(c *gin.Context) {
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

	streams, err := h.Holodex.GetUpcomingStreamsByOrg(ctx, 24, org)
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
func (h *StreamHandler) GetChannelStats(c *gin.Context) {
	ctx := c.Request.Context()

	if h.ValkeyCache != nil {
		var cachedStats map[string]*youtube.ChannelStats
		if err := h.ValkeyCache.Get(ctx, ChannelStatsCacheKey, &cachedStats); err != nil {
			h.respondInternalError(
				c,
				"Failed to get channel stats",
				"Failed to get channel stats from cache",
				err,
			)
			return
		}
		if cachedStats != nil {
			h.Logger.Debug("Channel stats cache hit", slog.Int("count", len(cachedStats)))
			c.JSON(200, gin.H{"status": "ok", "stats": cachedStats})
			return
		}
	}

	if h.StatsRepo != nil {
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
			h.Logger.Debug("Channel stats DB snapshot hit", slog.Int("count", len(stats)))

			h.cacheChannelStatsAsync(ctx, stats)
			h.triggerChannelStatsRefreshAsync(ctx)

			c.JSON(200, gin.H{"status": "ok", "stats": stats, "source": "db_snapshot"})
			return
		}
	}

	h.respondError(c, 503, "Channel stats snapshot not ready", gin.H{
		"code": "channel_stats_snapshot_not_ready",
		"hint": "retry later after background poller sync",
	})
}

func (h *StreamHandler) getChannelStatsFromDB(ctx context.Context) (map[string]*youtube.ChannelStats, error) {
	channelIDs, channelToName, err := h.GetActiveMemberIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("get members: %w", err)
	}

	if len(channelIDs) == 0 {
		return make(map[string]*youtube.ChannelStats), nil
	}

	dbStats, err := h.StatsRepo.GetLatestStatsForChannels(ctx, channelIDs)
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

func (h *StreamHandler) cacheChannelStatsAsync(ctx context.Context, stats map[string]*youtube.ChannelStats) {
	if h.ValkeyCache == nil || stats == nil {
		return
	}

	state := h.ensureState()
	h.runAsyncWithLimiter(state.channelStatsCacheLimiter, "cache_channel_stats", func() {
		cacheCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			constants.YouTubeConfig.CacheSaveTimeout,
		)
		defer cancel()

		if err := h.ValkeyCache.Set(cacheCtx, ChannelStatsCacheKey, stats, ChannelStatsCacheTTL); err != nil {
			h.Logger.Warn("Failed to cache channel stats", slog.Any("error", err))
		}
	})
}

func (h *StreamHandler) triggerChannelStatsRefreshAsync(ctx context.Context) {
	if h.ValkeyCache == nil || h.YouTube == nil {
		return
	}

	state := h.ensureState()
	h.runAsyncWithLimiter(state.channelStatsRefreshLimiter, "refresh_channel_stats", func() {
		bgCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			constants.YouTubeConfig.ScraperPhaseTimeout,
		)
		defer cancel()

		acquired, err := h.ValkeyCache.SetNX(bgCtx, ChannelStatsRefreshLockKey, ChannelStatsRefreshLockValue, ChannelStatsRefreshLockTTL)
		if err != nil {
			h.Logger.Warn("Failed to acquire refresh lock", slog.Any("error", err))
			return
		}
		if !acquired {
			h.Logger.Debug("Refresh lock already held, skipping background refresh")
			return
		}

		h.Logger.Info("Background channel stats refresh started")

		channelIDs, _, err := h.GetActiveMemberIndex(bgCtx)
		if err != nil {
			h.Logger.Warn("Background refresh: failed to get members", slog.Any("error", err))
			return
		}

		stats, err := h.YouTube.GetChannelStatistics(bgCtx, channelIDs)
		if err != nil {
			h.Logger.Warn("Background refresh: failed to get stats", slog.Any("error", err))
			return
		}

		h.cacheChannelStatsAsync(bgCtx, stats)
		h.Logger.Info("Background channel stats refresh completed", slog.Int("count", len(stats)))
	})
}

func (h *StreamHandler) runAsyncWithLimiter(limiter chan struct{}, task string, fn func()) {
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
		h.Logger.Debug("Skip async task: limiter saturated", slog.String("task", task))
	}
}

// GetActiveMemberIndex: 활성 멤버 인덱스를 조회합니다.
func (h *StreamHandler) GetActiveMemberIndex(ctx context.Context) ([]string, map[string]string, error) {
	state := h.ensureState()
	now := time.Now()

	state.memberIndexMu.RLock()
	if state.memberIndexReady && now.Before(state.memberIndexExpiresAt) {
		channelIDs := append([]string(nil), state.memberChannelIDs...)
		channelToName := maps.Clone(state.memberChannelName)
		state.memberIndexMu.RUnlock()
		return channelIDs, channelToName, nil
	}
	state.memberIndexMu.RUnlock()

	value, err, _ := state.memberIndexBuildGroup.Do("refresh", func() (any, error) {
		state.memberIndexMu.RLock()
		if state.memberIndexReady && time.Now().Before(state.memberIndexExpiresAt) {
			channelIDs := append([]string(nil), state.memberChannelIDs...)
			channelToName := maps.Clone(state.memberChannelName)
			state.memberIndexMu.RUnlock()
			return memberIndexSnapshot{channelIDs: channelIDs, channelNames: channelToName}, nil
		}
		state.memberIndexMu.RUnlock()

		members, loadErr := h.fetchAllMembers(ctx)
		if loadErr != nil {
			return nil, loadErr
		}

		channelIDs, channelToName := BuildActiveMemberIndex(members)

		state.memberIndexMu.Lock()
		state.memberChannelIDs = append([]string(nil), channelIDs...)
		state.memberChannelName = maps.Clone(channelToName)
		state.memberIndexExpiresAt = time.Now().Add(MemberIndexCacheTTL)
		state.memberIndexReady = true
		state.memberIndexMu.Unlock()

		return memberIndexSnapshot{channelIDs: channelIDs, channelNames: channelToName}, nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("member index singleflight: %w", err)
	}

	snapshot, ok := value.(memberIndexSnapshot)
	if !ok {
		return nil, nil, fmt.Errorf("member index snapshot: unexpected type")
	}

	return snapshot.channelIDs, snapshot.channelNames, nil
}

func (h *StreamHandler) fetchAllMembers(ctx context.Context) ([]*domain.Member, error) {
	if h.MemberIndexLoader == nil {
		return nil, fmt.Errorf("load members: repository loader is nil")
	}

	members, err := h.MemberIndexLoader(ctx)
	if err != nil {
		return nil, fmt.Errorf("load members: get all members: %w", err)
	}

	return members, nil
}

// BuildActiveMemberIndex: 비졸업 멤버의 channelID/name 인덱스를 구성합니다.
func BuildActiveMemberIndex(members []*domain.Member) ([]string, map[string]string) {
	channelIDs := make([]string, 0, len(members))
	channelToName := make(map[string]string, len(members))
	for _, member := range members {
		if member.ChannelID == "" || member.IsGraduated {
			continue
		}
		channelIDs = append(channelIDs, member.ChannelID)
		channelToName[member.ChannelID] = member.Name
	}

	return channelIDs, channelToName
}

// GetChannel: channelIds 파라미터로 여러 채널을 한 번에 조회합니다.
func (h *StreamHandler) GetChannel(c *gin.Context) {
	ctx := c.Request.Context()

	channelIDs := c.Query("channelIds")
	if channelIDs != "" {
		ids := SplitChannelIDs(channelIDs)
		if len(ids) == 0 {
			h.respondError(c, 400, "channelIds parameter is empty or invalid", nil)
			return
		}

		if len(ids) > 100 {
			h.respondError(c, 400, "channelIds supports at most 100 values", gin.H{
				"limit":    100,
				"received": len(ids),
			})
			return
		}

		channelsMap, err := h.MemberRepo.GetMembersWithPhoto(ctx, ids)
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

		channels := make([]*ChannelResponse, 0, len(channelsMap))
		for _, member := range channelsMap {
			channels = append(channels, MemberToChannelResponse(member))
		}

		c.JSON(200, gin.H{"status": "ok", "channels": channels})
		return
	}

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

// MemberToChannelResponse: domain.Member를 채널 API 응답 형식으로 변환합니다.
func MemberToChannelResponse(m *domain.Member) *ChannelResponse {
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

// SearchChannels: 이름으로 채널을 검색합니다.
func (h *StreamHandler) SearchChannels(c *gin.Context) {
	ctx := c.Request.Context()
	query := c.Query("q")
	if query == "" {
		h.respondError(c, 400, "q parameter required", nil)
		return
	}

	channels, err := h.Holodex.SearchChannels(ctx, query)
	if err != nil {
		h.respondInternalError(c, "Failed to search channels", "Failed to search channels", err,
			slog.String("query", query))
		return
	}

	c.JSON(200, gin.H{"status": "ok", "channels": channels})
}
