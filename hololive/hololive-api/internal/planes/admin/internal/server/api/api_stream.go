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

package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

const (
	channelStatsCacheWorkers   = sharedserver.DefaultChannelStatsCacheWorkers
	channelStatsRefreshWorkers = sharedserver.DefaultChannelStatsRefreshWorkers
)

func (h *StreamHandler) sharedStreamHandler() *sharedserver.StreamHandler {
	var api *Handler
	if h != nil {
		api = h.Handler
	}

	handler := &sharedserver.StreamHandler{
		Logger:       api.safeLogger(),
		State:        api.ensureStreamState(),
		RespondError: sharedserver.RespondError,
		RespondInternalError: func(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr) {
			sharedserver.RespondInternalError(api.safeLogger(), c, userMessage, logMessage, err, attrs...)
		},
	}

	if api != nil {
		handler.Holodex = api.holodex
		handler.YouTube = api.youtube
		handler.ValkeyCache = api.valkeyCache
		handler.MemberRepository = api.repository
		handler.MemberIndexLoader = api.memberIndexLoader
	}

	return handler
}

func (h *StreamHandler) GetLiveStreams(c *gin.Context) {
	h.sharedStreamHandler().GetLiveStreams(c)
}

func (h *StreamHandler) GetUpcomingStreams(c *gin.Context) {
	h.sharedStreamHandler().GetUpcomingStreams(c)
}

func (h *StreamHandler) getActiveMemberIndex(ctx context.Context) (ids []string, names map[string]string, err error) {
	ids, names, err = h.sharedStreamHandler().GetActiveMemberIndex(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get active member index: %w", err)
	}

	return ids, names, nil
}

func (h *MemberHandler) invalidateMemberIndex() {
	h.ensureStreamState().InvalidateMemberIndex()
}

func (h *StreamHandler) GetChannel(c *gin.Context) {
	h.sharedStreamHandler().GetChannel(c)
}

func (h *StreamHandler) SearchChannels(c *gin.Context) {
	h.sharedStreamHandler().SearchChannels(c)
}
