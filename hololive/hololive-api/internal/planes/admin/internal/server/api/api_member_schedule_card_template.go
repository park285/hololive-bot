package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/server/ginjson"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type memberCardTemplateResponse struct {
	Template cardtemplate.ID `json:"template"`
}

func (h *SchedulePosterHandler) MemberScheduleCardTemplate(c *gin.Context) {
	if !h.ready(c) {
		return
	}

	rawID := c.Param("id")
	id, err := strconv.ParseInt(rawID, 10, 32)

	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != rawID {
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid member id", nil)

		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), schedulePosterTimeout)
	defer cancel()

	var value cardtemplate.ID

	switch c.Request.Method {
	case http.MethodGet:
		value, err = h.store.MemberCardTemplate(ctx, int(id))
	case http.MethodPatch:
		var request struct {
			ExpectedTemplate cardtemplate.ID `json:"expectedTemplate"`
			Template         cardtemplate.ID `json:"template"`
		}

		if !decodeSchedulePosterChange(c, &request) {
			return
		}

		value = request.Template
		err = h.store.SetMemberCardTemplate(ctx, int(id), request.ExpectedTemplate, value)
	default:
		c.Status(http.StatusMethodNotAllowed)

		return
	}

	switch {
	case err == nil:
		ginjson.Respond(c, http.StatusOK, memberCardTemplateResponse{Template: value})
	case errors.Is(err, domain.ErrMemberNotFound):
		sharedserver.RespondError(c, http.StatusNotFound, "member not found", nil)
	case errors.Is(err, scheduleposter.ErrInvalidCardTemplate):
		sharedserver.RespondError(c, http.StatusBadRequest, "invalid schedule card template", nil)
	default:
		h.respondStoreError(c, "member schedule card template", err)
	}
}
