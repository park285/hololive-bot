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

package handlers

import (
	stdErrors "errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

type StreamRespondErrorFunc func(c *gin.Context, status int, message string, extra gin.H)

type StreamRespondInternalErrorFunc func(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr)

type StreamHandler struct {
	Logger               *slog.Logger
	Holodex              *holodexprovider.Service
	RespondError         StreamRespondErrorFunc
	RespondInternalError StreamRespondInternalErrorFunc
}

func (h *StreamHandler) respondBadRequest(c *gin.Context, message string, extra gin.H) {
	if h.RespondError != nil {
		h.RespondError(c, http.StatusBadRequest, message, extra)

		return
	}

	sharedserver.RespondError(c, http.StatusBadRequest, message, extra)
}

func (h *StreamHandler) respondInternalError(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr) {
	if h.RespondInternalError != nil {
		h.RespondInternalError(c, userMessage, logMessage, err, attrs...)

		return
	}

	sharedserver.RespondInternalError(h.Logger, c, userMessage, logMessage, err, attrs...)
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

	ginjson.Respond(c, http.StatusOK, gin.H{"status": "ok", "org": org, "streams": streams})
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

	ginjson.Respond(c, http.StatusOK, gin.H{"status": "ok", "org": org, "streams": streams})
}
