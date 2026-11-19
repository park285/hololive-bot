package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/kapu/hololive-shared/pkg/service/xspaces"
)

type xSpaceSessionResponse struct {
	Status     string         `json:"status"`
	Connection xspaces.Status `json:"connection"`
	Accepted   bool           `json:"accepted"`
}

func (h *SettingsHandler) GetXSpaceSession(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	status, err := h.xSpaceSessions.Status(ctx)
	if err != nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "X session status unavailable", nil)

		return
	}

	ginjson.Respond(c, http.StatusOK, xSpaceSessionResponse{Status: "ok", Connection: status})
}

func (h *SettingsHandler) SubmitXSpaceSession(c *gin.Context) {
	var request struct {
		AuthToken        string `json:"authToken"`
		CSRFToken        string `json:"csrfToken"`
		ExpectedRevision string `json:"expectedRevision"`
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)

	if err := bindJSON(c, &request); err != nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid X session request", nil)

		return
	}

	cookies := xspaces.Cookies{AuthToken: request.AuthToken, CSRFToken: request.CSRFToken}
	if err := cookies.Validate(); err != nil {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid X session cookies", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)

	defer cancel()

	accepted := false

	if h.xSpaceSessions != nil {
		var err error

		accepted, err = h.xSpaceSessions.Submit(ctx, cookies, request.ExpectedRevision)
		if err != nil {
			sharedserver.RespondError(c, http.StatusServiceUnavailable, "X session submission outcome unconfirmed; read status", nil)

			return
		}
	}

	status, err := h.xSpaceSessions.Status(ctx)
	if err != nil {
		sharedserver.RespondError(c, http.StatusServiceUnavailable, "X session submission outcome unconfirmed; read status", nil)

		return
	}

	ginjson.Respond(c, http.StatusOK, xSpaceSessionResponse{Status: "ok", Connection: status, Accepted: accepted})
}
