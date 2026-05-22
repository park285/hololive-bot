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

package httpserver

import (
	"context"
	stdErrors "errors"
	"log/slog"
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
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
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

func (s *StreamState) InvalidateMemberIndex() {
	s.memberIndexMu.Lock()
	defer s.memberIndexMu.Unlock()

	s.memberChannelIDs = nil
	s.memberChannelName = make(map[string]string)
	s.memberIndexExpiresAt = time.Time{}
	s.memberIndexReady = false
}

type StreamMemberRepository interface {
	GetMembersWithPhoto(ctx context.Context, channelIDs []string) (map[string]*domain.Member, error)
}

type StreamRespondErrorFunc func(c *gin.Context, status int, message string, extra gin.H)

type StreamRespondInternalErrorFunc func(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr)

type StreamHandler struct {
	Logger               *slog.Logger
	Holodex              *holodex.Service
	YouTube              youtube.Service
	ValkeyCache          cache.Client
	StatsRepository            ytstats.StatsDashboardRepository
	MemberRepository           StreamMemberRepository
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
