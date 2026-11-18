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
