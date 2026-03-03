package server

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

const channelStatsCacheWorkers = sharedserver.DefaultChannelStatsCacheWorkers
const channelStatsRefreshWorkers = sharedserver.DefaultChannelStatsRefreshWorkers

func (h *StreamAPIHandler) sharedStreamHandler() *sharedserver.StreamHandler {
	return &sharedserver.StreamHandler{
		Logger:               h.logger,
		Holodex:              h.holodex,
		YouTube:              h.youtube,
		ValkeyCache:          h.valkeyCache,
		StatsRepo:            h.statsRepo,
		MemberRepo:           h.repo,
		MemberIndexLoader:    h.memberIndexLoader,
		State:                h.ensureStreamState(),
		RespondError:         h.respondError,
		RespondInternalError: h.respondInternalError,
	}
}

// GetLiveStreams: 현재 라이브 방송 중인 스트림 목록을 반환합니다.
func (h *StreamAPIHandler) GetLiveStreams(c *gin.Context) {
	h.sharedStreamHandler().GetLiveStreams(c)
}

// GetUpcomingStreams: 예정된 스트림 목록을 반환합니다.
func (h *StreamAPIHandler) GetUpcomingStreams(c *gin.Context) {
	h.sharedStreamHandler().GetUpcomingStreams(c)
}

// GetChannelStats: 채널 통계를 반환합니다. (SWR 패턴: 캐시 → DB → 백그라운드 갱신)
func (h *StreamAPIHandler) GetChannelStats(c *gin.Context) {
	h.sharedStreamHandler().GetChannelStats(c)
}

func (h *StreamAPIHandler) getActiveMemberIndex(ctx context.Context) ([]string, map[string]string, error) {
	ids, names, err := h.sharedStreamHandler().GetActiveMemberIndex(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get active member index: %w", err)
	}
	return ids, names, nil
}

func (h *MemberAPIHandler) invalidateMemberIndex() {
	h.ensureStreamState().InvalidateMemberIndex()
}

// GetChannel: channelIds 파라미터로 여러 채널을 한 번에 조회합니다.
func (h *StreamAPIHandler) GetChannel(c *gin.Context) {
	h.sharedStreamHandler().GetChannel(c)
}

// SearchChannels: 이름으로 채널을 검색합니다.
func (h *StreamAPIHandler) SearchChannels(c *gin.Context) {
	h.sharedStreamHandler().SearchChannels(c)
}
