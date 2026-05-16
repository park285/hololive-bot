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
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (h *StreamHandler) GetChannel(c *gin.Context) {
	channelIDs := c.Query("channelIds")
	if channelIDs != "" {
		h.getChannelsByIDs(c, channelIDs)
		return
	}

	h.respondChannelQueryError(c)
}

func (h *StreamHandler) getChannelsByIDs(c *gin.Context, channelIDs string) {
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

	channelsMap, err := h.MemberRepo.GetMembersWithPhoto(c.Request.Context(), ids)
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

	c.JSON(200, gin.H{"status": "ok", "channels": channelResponses(channelsMap)})
}

func (h *StreamHandler) respondChannelQueryError(c *gin.Context) {
	channelID := c.Query("channelId")
	if channelID != "" {
		h.respondError(c, 410, "Legacy channelId query is no longer supported", gin.H{
			"hint": "use channelIds query parameter",
		})
		return
	}
	h.respondError(c, 400, "channelIds parameter required", nil)
}

func channelResponses(channelsMap map[string]*domain.Member) []*ChannelResponse {
	channels := make([]*ChannelResponse, 0, len(channelsMap))
	for _, member := range channelsMap {
		channels = append(channels, MemberToChannelResponse(member))
	}
	return channels
}

type ChannelResponse struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Photo *string `json:"photo,omitempty"`
}

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
