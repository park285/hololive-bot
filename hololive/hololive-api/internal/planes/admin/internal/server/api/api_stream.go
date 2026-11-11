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
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

type StreamHandler struct {
	holodex *holodexprovider.Service
	logger  *slog.Logger
}

func (h *StreamHandler) GetLiveStreams(c *gin.Context) {
	org, ok := requestedStreamOrg(c)
	if !ok {
		return
	}

	streams, err := h.holodex.GetLiveStreamsByOrg(c.Request.Context(), org)
	if err != nil {
		h.respondStreamsError(c, err, "Failed to get live streams")

		return
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"status": "ok", "org": org, "streams": streams})
}

func (h *StreamHandler) GetUpcomingStreams(c *gin.Context) {
	org, ok := requestedStreamOrg(c)
	if !ok {
		return
	}

	streams, err := h.holodex.GetUpcomingStreamsByOrg(c.Request.Context(), 24, org)
	if err != nil {
		h.respondStreamsError(c, err, "Failed to get upcoming streams")

		return
	}

	ginjson.Respond(c, http.StatusOK, gin.H{"status": "ok", "org": org, "streams": streams})
}

// requestedStreamOrg는 org query가 없으면 Hololive를 쓰고, 공백뿐인 값은 400으로 거절한다.
// 지원하지 않는 org 판정은 provider가 ErrInvalidStreamOrg로 돌려준다.
func requestedStreamOrg(c *gin.Context) (string, bool) {
	rawOrg, hasOrg := c.GetQuery("org")
	if !hasOrg {
		return constants.HolodexAPIParams.OrgHololive, true
	}

	org := strings.TrimSpace(rawOrg)
	if org == "" {
		respondInvalidStreamOrg(c)

		return "", false
	}

	return org, true
}

func respondInvalidStreamOrg(c *gin.Context) {
	sharedserver.RespondError(c, http.StatusBadRequest, "Invalid org parameter", gin.H{
		"default_org":    strings.ToLower(constants.HolodexAPIParams.OrgHololive),
		"supported_orgs": holodexprovider.SupportedStreamOrgParams(),
	})
}

func (h *StreamHandler) respondStreamsError(c *gin.Context, err error, message string) {
	if errors.Is(err, holodexprovider.ErrInvalidStreamOrg) {
		respondInvalidStreamOrg(c)

		return
	}

	sharedserver.RespondInternalError(h.safeLogger(), c, message, message, err)
}
