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
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

const (
	ChannelStatsCacheKey = "admin:channel_stats"
	ChannelStatsCacheTTL = 10 * time.Minute

	ChannelStatsRefreshLockKey   = "admin:channel_stats:refresh_lock"
	ChannelStatsRefreshLockValue = "locked"
	ChannelStatsRefreshLockTTL   = 5 * time.Minute

	MemberIndexCacheTTL = 1 * time.Minute

	MemberIndexRefreshTimeout = 30 * time.Second

	DefaultChannelStatsCacheWorkers   = 4
	DefaultChannelStatsRefreshWorkers = 1
)

// 저장 즉시 불변으로 취급하고 통째로 교체한다. 필드를 제자리 수정하면
// 이미 반환된 스냅샷을 공유하는 호출자에게 data race가 발생한다.
type memberIndexSnapshot struct {
	channelIDs   []string
	channelNames map[string]string
	expiresAt    time.Time
}

type StreamState struct {
	memberIndex           atomic.Pointer[memberIndexSnapshot]
	memberIndexBuildGroup singleflight.Group
}

func NewStreamState(_, _ int) *StreamState {
	return &StreamState{}
}

func (s *StreamState) InvalidateMemberIndex() {
	s.memberIndex.Store(nil)
}

type StreamMemberRepository interface {
	GetMembersWithPhoto(ctx context.Context, channelIDs []string) (map[string]*domain.Member, error)
}

type StreamRespondErrorFunc func(c *gin.Context, status int, message string, extra gin.H)

type StreamRespondInternalErrorFunc func(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr)

type StreamHandler struct {
	Logger               *slog.Logger
	Holodex              *holodexprovider.Service
	YouTube              youtube.Service
	ValkeyCache          cache.KeyValueCache
	MemberRepository     StreamMemberRepository
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

func (h *StreamHandler) respondBadRequest(c *gin.Context, message string, extra gin.H) {
	if h.RespondError != nil {
		h.RespondError(c, 400, message, extra)

		return
	}

	RespondError(c, 400, message, extra)
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
			h.respondBadRequest(c, "Invalid org parameter", gin.H{
				"default_org":    strings.ToLower(constants.HolodexAPIParams.OrgHololive),
				"supported_orgs": holodexprovider.SupportedStreamOrgParams(),
			})

			return
		}
	}

	streams, err := h.Holodex.GetLiveStreamsByOrg(ctx, org)
	if err != nil {
		if stdErrors.Is(err, holodexprovider.ErrInvalidStreamOrg) {
			h.respondBadRequest(c, "Invalid org parameter", gin.H{
				"default_org":    strings.ToLower(constants.HolodexAPIParams.OrgHololive),
				"supported_orgs": holodexprovider.SupportedStreamOrgParams(),
			})

			return
		}

		h.respondInternalError(c, "Failed to get live streams", "Failed to get live streams", err)

		return
	}

	respondJSON(c, 200, gin.H{responseKeyStatus: "ok", "org": org, "streams": streams})
}

func (h *StreamHandler) GetUpcomingStreams(c *gin.Context) {
	ctx := c.Request.Context()
	org := constants.HolodexAPIParams.OrgHololive

	if rawOrg, hasOrg := c.GetQuery("org"); hasOrg {
		org = strings.TrimSpace(rawOrg)
		if org == "" {
			h.respondBadRequest(c, "Invalid org parameter", gin.H{
				"default_org":    strings.ToLower(constants.HolodexAPIParams.OrgHololive),
				"supported_orgs": holodexprovider.SupportedStreamOrgParams(),
			})

			return
		}
	}

	streams, err := h.Holodex.GetUpcomingStreamsByOrg(ctx, 24, org)
	if err != nil {
		if stdErrors.Is(err, holodexprovider.ErrInvalidStreamOrg) {
			h.respondBadRequest(c, "Invalid org parameter", gin.H{
				"default_org":    strings.ToLower(constants.HolodexAPIParams.OrgHololive),
				"supported_orgs": holodexprovider.SupportedStreamOrgParams(),
			})

			return
		}

		h.respondInternalError(c, "Failed to get upcoming streams", "Failed to get upcoming streams", err)

		return
	}

	respondJSON(c, 200, gin.H{responseKeyStatus: "ok", "org": org, "streams": streams})
}
